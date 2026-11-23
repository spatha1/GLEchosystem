<script lang="ts">
	// Client onboarding: register the client in GL, prepare its empty draft configuration and
	// give its team their roles, so Stage 1 is ready for the analyst. By hand, or from the AI
	// assistant's suggestions, which fill the form for the administrator to review. The tenant
	// itself (sign-in) comes from the platform; no accounting data is created here.
	import { wsClient, roleLabels, allRoles, type ClientProfile, type OnboardingProfile, type Suggestion } from './api';
	import type { WsRoute } from './route';
	import type { ConfigSummary, Me } from '../lib/types';
	import ReasonDialog from '../components/ReasonDialog.svelte';
	import AIPanel from './AIPanel.svelte';

	let { list, mine, client, go }: { list: { code: string; label: string }[]; mine: Record<string, Me | null>; client: string; go: (r: WsRoute) => void } = $props();

	const admins = $derived(list.filter((c) => mine[c.code]?.roles.includes('GL_ADMIN')));
	const sel = $derived(admins.find((c) => c.code === client)?.code ?? admins[0]?.code ?? '');
	const api = $derived(wsClient(sel));
	const me = $derived(mine[sel]?.actor ?? '');

	let existing = $state<ClientProfile | null>(null);
	let configs = $state<ConfigSummary[]>([]);
	let people = $state<{ actor: string; roles: string[] }[]>([]);
	let loaded = $state(false);
	let error = $state('');
	let done = $state<string[]>([]);
	let asking = $state(false);
	let saving = $state(false);
	let fromAI = $state<string[]>([]);

	let f = $state({ name: '', status: 'ONBOARDING', configId: '', effectiveFrom: '', description: '', systems: '', reference: '', initialize: true });
	let params = $state<{ k: string; v: string }[]>([]);
	let team = $state<{ user: string; roles: string[] }[]>([]);

	async function load() {
		if (!sel) return;
		loaded = false;
		error = '';
		done = [];
		fromAI = [];
		const label = list.find((c) => c.code === sel)?.label ?? sel;
		try {
			const [p, cs, rs] = await Promise.all([api.getClient().catch(() => null), api.configs(), api.roles()]);
			existing = p;
			configs = cs;
			people = rs.map((r) => ({ actor: r.actor, roles: r.roles.map((x) => x.role) }));
			const pr: OnboardingProfile = p?.profile ?? {};
			f = {
				name: p?.name ?? label,
				status: p?.status ?? 'ONBOARDING',
				configId: p?.configId ?? '',
				effectiveFrom: pr.effectiveFrom ?? '',
				description: pr.description ?? '',
				systems: (pr.sourceSystems ?? []).join(', '),
				reference: pr.reference ?? '',
				initialize: true
			};
			params = Object.entries(pr.parameters ?? {}).map(([k, v]) => ({ k, v }));
			team = pr.team?.length ? pr.team.map((t) => ({ ...t })) : people.filter((x) => x.actor !== me).map((x) => ({ user: x.actor, roles: [...x.roles] }));
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loaded = true;
		}
	}
	$effect(() => {
		sel;
		load();
	});

	const configExists = $derived(configs.some((c) => c.configId === f.configId));
	const valid = $derived(!!f.name.trim() && /^[A-Z][A-Z0-9_]*$/.test(f.configId) && (configExists || !f.initialize || /^\d{4}-\d{2}-\d{2}$/.test(f.effectiveFrom)));
	const missing = $derived(
		[
			!f.name.trim() && 'the client’s name',
			!/^[A-Z][A-Z0-9_]*$/.test(f.configId) && 'a configuration code (upper case, e.g. CLIENT_GL)',
			!configExists && f.initialize && !/^\d{4}-\d{2}-\d{2}$/.test(f.effectiveFrom) && 'the date the configuration applies from',
			!team.some((t) => t.roles.includes('CONFIG_EDITOR')) && 'a business analyst',
			!team.some((t) => t.roles.includes('CONFIG_APPROVER')) && 'a reviewer (independent of the analysts)',
			!f.systems.trim() && 'the source systems'
		].filter(Boolean) as string[]
	);

	function profile(): OnboardingProfile {
		return {
			description: f.description.trim() || undefined,
			sourceSystems: f.systems.split(',').map((x) => x.trim().toUpperCase()).filter(Boolean),
			parameters: Object.fromEntries(params.filter((p) => p.k.trim()).map((p) => [p.k.trim(), p.v.trim()])),
			team: team.filter((t) => t.user.trim()).map((t) => ({ user: t.user.trim().toLowerCase(), roles: t.roles })),
			reference: f.reference || undefined,
			effectiveFrom: f.effectiveFrom || undefined
		};
	}

	async function save(reason: string) {
		saving = true;
		error = '';
		const out: string[] = [];
		try {
			const r = await api.putClient({
				name: f.name.trim(),
				status: f.status,
				configId: f.configId,
				profile: profile(),
				reason,
				initialize: f.initialize && !configExists ? { title: `${f.name.trim()} GL`, effectiveFrom: f.effectiveFrom } : undefined
			});
			out.push(existing ? 'Client profile updated' : `Client ${r.client.name} onboarded (${r.client.status.toLowerCase()})`);
			if (r.initialized) out.push(`Draft configuration ${r.initialized} v1 created, empty, for the analysts`);
			for (const t of profile().team ?? []) {
				if (t.user === me) continue;
				const now = people.find((p) => p.actor === t.user)?.roles ?? [];
				if ([...now].sort().join() === [...t.roles].sort().join()) continue;
				await api.setRoles(t.user, t.roles, reason);
				out.push(`${t.user}: ${t.roles.map((x) => roleLabels[x]).join(', ') || 'no role'}`);
			}
			asking = false;
			await load();
			done = out;
		} catch (e) {
			error = (e as Error).message;
			asking = false;
		} finally {
			saving = false;
		}
	}

	function aiAccept(s: Suggestion) {
		const d = s.data;
		if (s.kind === 'client') {
			if (d.name) f.name = String(d.name);
			if (d.configId) f.configId = String(d.configId).toUpperCase();
			if (d.effectiveFrom) f.effectiveFrom = String(d.effectiveFrom);
			if (d.description) f.description = String(d.description);
			if (Array.isArray(d.sourceSystems)) f.systems = d.sourceSystems.join(', ');
			if (d.parameters && typeof d.parameters === 'object') params = [...params, ...Object.entries(d.parameters).map(([k, v]) => ({ k, v: String(v) }))];
			fromAI = [...fromAI, 'client details'];
			return 'Filled into the form: review it before onboarding.';
		}
		if (s.kind === 'roles' && typeof d.user === 'string') {
			const u = d.user.trim().toLowerCase();
			const roles = (d.roles ?? []).filter((r: string) => allRoles.includes(r));
			const i = team.findIndex((t) => t.user === u);
			if (i >= 0) team[i] = { user: u, roles };
			else team = [...team, { user: u, roles }];
			fromAI = [...fromAI, 'team'];
			return 'Added to the team: review it before onboarding.';
		}
	}
</script>

<h1 class="ws-h1">Client onboarding</h1>
<p class="ws-lead">Register the client in GL, prepare its empty draft configuration and give its team their roles. Stage 1 then opens for the business analyst. Nothing is decided for you: the assistant fills the form, you review and confirm.</p>

{#if !admins.length}
	<div class="card"><p>Onboarding is for administrators of a client.</p></div>
{:else}
	<div class="row picks">
		{#each admins as c (c.code)}<button class="btn" class:primary={c.code === sel} onclick={() => go({ page: 'onboard', client: c.code })}>{c.label}</button>{/each}
	</div>
	<div class="with-ai">
		<div class="stack">
			{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
			{#if done.length}
				<section class="card ok" aria-label="Onboarding result">
					<h2 class="sec">Done</h2>
					<ul class="checks">{#each done as d (d)}<li>✓ {d}</li>{/each}</ul>
					<div class="row"><button class="btn primary" onclick={() => go({ page: 'stage1', client: sel, step: 1 })}>Open Stage 1 for {f.name}</button></div>
				</section>
			{/if}
			{#if !loaded}<p class="muted">Loading…</p>{:else}
				<section class="card" aria-label="Client">
					<div class="row head">
						<h2 class="sec">Client</h2>
						{#if existing}<span class="pill {existing.status === 'ACTIVE' ? 'ok' : 'info'}">{existing.status === 'ACTIVE' ? 'Active' : 'Onboarding'}</span><span class="small muted">since {existing.createdAt.slice(0, 10)} by {existing.createdBy}</span>{:else}<span class="pill warn">Not onboarded yet</span>{/if}
						{#if fromAI.length}<span class="pill ai">from the assistant: {[...new Set(fromAI)].join(', ')}</span>{/if}
					</div>
					<div class="form">
						<label class="lbl">Client name <input class="field" bind:value={f.name} /></label>
						<label class="lbl">Status
							<select class="field" bind:value={f.status}><option value="ONBOARDING">Onboarding</option><option value="ACTIVE">Active</option><option value="SUSPENDED">Suspended</option></select></label
						>
						<label class="lbl">Configuration code <input class="field mono" bind:value={f.configId} placeholder="e.g. {sel}_GL" oninput={() => (f.configId = f.configId.toUpperCase())} /></label>
						<label class="lbl">Applies from <input class="field" type="date" bind:value={f.effectiveFrom} /></label>
						<label class="lbl wide">What the client needs <textarea class="field" rows="3" bind:value={f.description} placeholder="Ledgers, companies, currencies, the systems that feed GL…"></textarea></label>
						<label class="lbl wide">Source systems <input class="field" bind:value={f.systems} placeholder="POLICY, BILLING, CLAIMS" /></label>
						<label class="lbl">Reference configuration (optional)
							<select class="field" bind:value={f.reference}><option value="">none</option>{#each configs as c (c.configId)}<option value={c.configId}>{c.configId} · {c.title}</option>{/each}</select></label
						>
						<label class="lbl chk">
							<span><input type="checkbox" bind:checked={f.initialize} disabled={configExists} /> Create the empty draft configuration now</span>
							<span class="small muted">{configExists ? `${f.configId} already exists: Stage 1 works on it.` : 'Version 1, empty: the analyst fills it in Stage 1.'}</span>
						</label>
					</div>
					<details class="adv">
						<summary>Client parameters ({params.length})</summary>
						{#each params as p, i (i)}
							<div class="row"><input class="field" bind:value={p.k} placeholder="name" /><input class="field grow" bind:value={p.v} placeholder="value" /><button class="btn ghost small" onclick={() => (params = params.filter((_, j) => j !== i))}>Remove</button></div>
						{/each}
						<button class="btn small" onclick={() => (params = [...params, { k: '', v: '' }])}>+ Parameter</button>
					</details>
				</section>

				<section class="card" aria-label="Team">
					<h2 class="sec">Team and roles</h2>
					<p class="small muted">The reviewer must be someone other than the analysts: nobody approves what they edited.</p>
					<div class="tw">
						<table class="grid">
							<thead><tr><th>Person</th>{#each allRoles as r (r)}<th class="c">{roleLabels[r]}</th>{/each}<th></th></tr></thead>
							<tbody>
								{#each team as t, i (i)}
									<tr>
										<td><input class="field" style="width:100%;min-width:230px;box-sizing:border-box" bind:value={t.user} placeholder="name@company.com" aria-label="Team member" /></td>
										{#each allRoles as r (r)}<td class="c"><input type="checkbox" checked={t.roles.includes(r)} onchange={() => (t.roles = t.roles.includes(r) ? t.roles.filter((x) => x !== r) : [...t.roles, r])} /></td>{/each}
										<td><button class="btn ghost small" onclick={() => (team = team.filter((_, j) => j !== i))}>Remove</button></td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
					<button class="btn small" onclick={() => (team = [...team, { user: '', roles: ['CONFIG_EDITOR'] }])}>+ Person</button>
				</section>

				<section class="card" aria-label="Ready">
					{#if missing.length}
						<p class="notice warn small">Still needed: {missing.join('; ')}.</p>
					{:else}
						<p class="notice ok small">Everything Stage 1 needs is here.</p>
					{/if}
					<div class="row">
						<button class="btn primary" disabled={!valid || saving} onclick={() => (asking = true)}>{existing ? 'Save changes…' : 'Onboard client…'}</button>
						{#if existing?.configId}<button class="btn" onclick={() => go({ page: 'client', client: sel })}>Open the client workspace</button>{/if}
					</div>
				</section>
			{/if}
		</div>
		<AIPanel
			{api}
			area="onboarding"
			canAct={true}
			accept={aiAccept}
			examples={[
				'Onboard this client as a new GL client. Use the existing configuration where appropriate and prepare Stage 1.',
				'What onboarding information is still missing?',
				'Suggest the user roles for the configuration team.'
			]}
		/>
	</div>
{/if}

{#if asking}
	<ReasonDialog
		title={existing ? `Update ${f.name}` : `Onboard ${f.name}`}
		verb={existing ? 'Save' : 'Onboard'}
		detail={`${f.configId}${f.initialize && !configExists ? ' (empty draft v1 created)' : ''} · team of ${team.filter((t) => t.user.trim()).length}. Roles are granted with the same reason.`}
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = false)}
	/>
{/if}

<style>
	.picks {
		margin-bottom: 14px;
	}
	.head {
		margin-bottom: 8px;
	}
	.head h2 {
		margin: 0;
	}
	.form {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px 16px;
	}
	.form .wide {
		grid-column: 1 / -1;
	}
	.chk span {
		font-weight: 500;
	}
	.adv {
		margin-top: 12px;
	}
	.adv summary {
		cursor: pointer;
		color: var(--muted);
		font-weight: 600;
		font-size: 13px;
		margin-bottom: 8px;
	}
	.adv .row {
		margin-bottom: 6px;
	}
	.grow {
		flex: 1;
	}
	.c {
		text-align: center;
	}
	.ok {
		border-color: var(--ok);
	}
	.checks {
		list-style: none;
		padding: 0;
		margin: 0 0 10px;
		display: grid;
		gap: 4px;
	}
	@media (max-width: 700px) {
		.form {
			grid-template-columns: 1fr;
		}
	}
</style>
