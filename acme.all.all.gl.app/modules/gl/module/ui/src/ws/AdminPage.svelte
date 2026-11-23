<script lang="ts">
	// Administration, per client: who has which role, what each role may do (read from the
	// server's own route table), and the history. Only clients where you are an administrator
	// are listed; the server checks every change again.
	import { wsClient, roleLabels, allRoles, type ClientProfile, type Permissions, type Suggestion } from './api';
	import type { WsRoute } from './route';
	import type { Me, RoleAssignment, RoleAuditEntry } from '../lib/types';
	import ReasonDialog from '../components/ReasonDialog.svelte';
	import AIPanel from './AIPanel.svelte';

	let { list, mine, client, go }: { list: { code: string; label: string }[]; mine: Record<string, Me | null>; client: string; go: (r: WsRoute) => void } = $props();

	const admins = $derived(list.filter((c) => mine[c.code]?.roles.includes('GL_ADMIN')));
	const sel = $derived(admins.find((c) => c.code === client)?.code ?? admins[0]?.code ?? '');
	const api = $derived(wsClient(sel));
	const me = $derived(mine[sel]?.actor ?? '');

	let tab = $state<'users' | 'perms' | 'audit'>('users');
	let people = $state<RoleAssignment[]>([]);
	let profiles = $state<Record<string, ClientProfile | null>>({});
	let counts = $state<Record<string, number>>({});
	let perms = $state<Permissions | null>(null);
	let audit = $state<{ at: string; who: string; what: string; reason: string }[]>([]);
	let error = $state('');
	let editing = $state<Record<string, string[]>>({}); // user → proposed roles, not saved yet
	let newUser = $state('');
	let newRoles = $state<string[]>([]);
	let asking = $state<{ user: string; roles: string[] } | null>(null);
	let saving = $state(false);
	let saved = $state('');

	$effect(() => {
		for (const c of admins) {
			const a = wsClient(c.code);
			a.getClient().then(
				(p) => (profiles = { ...profiles, [c.code]: p }),
				() => (profiles = { ...profiles, [c.code]: null })
			);
			a.roles().then(
				(r) => (counts = { ...counts, [c.code]: r.length }),
				() => {}
			);
		}
	});

	async function load() {
		if (!sel) return;
		error = '';
		try {
			const [r, p, ra, ca] = await Promise.all([api.roles(), api.permissions(), api.roleAudit(), api.clientAudit().catch(() => [])]);
			people = r;
			perms = p;
			audit = [
				...ra.map((e: RoleAuditEntry) => ({
					at: e.at,
					who: e.actor,
					what: `${e.subject}: ${[...(e.granted ?? []).map((x) => '+ ' + (roleLabels[x] ?? x)), ...(e.revoked ?? []).map((x) => '− ' + (roleLabels[x] ?? x))].join(', ')}`,
					reason: e.reason
				})),
				...ca.map((e) => ({ at: e.at, who: e.actor, what: e.action === 'onboard' ? 'Onboarded the client' : 'Updated the client profile', reason: e.reason }))
			].sort((a, b) => b.at.localeCompare(a.at));
		} catch (e) {
			error = (e as Error).message;
		}
	}
	$effect(() => {
		sel;
		load();
	});

	const rolesOf = (u: string) => people.find((p) => p.actor === u)?.roles.map((r) => r.role) ?? [];
	function toggle(u: string, r: string) {
		const cur = editing[u] ?? rolesOf(u);
		editing = { ...editing, [u]: cur.includes(r) ? cur.filter((x) => x !== r) : [...cur, r] };
	}
	const changed = (u: string) => editing[u] && [...editing[u]].sort().join() !== [...rolesOf(u)].sort().join();

	async function save(reason: string) {
		if (!asking) return;
		saving = true;
		try {
			await api.setRoles(asking.user, asking.roles, reason);
			saved = `Saved: ${asking.user} now has ${asking.roles.map((r) => roleLabels[r]).join(', ') || 'no role'}.`;
			const { [asking.user]: _, ...rest } = editing;
			editing = rest;
			if (asking.user === newUser.trim()) {
				newUser = '';
				newRoles = [];
			}
			asking = null;
			await load();
		} catch (e) {
			error = (e as Error).message;
			asking = null;
		} finally {
			saving = false;
		}
	}

	// capabilities in business words, from the operations the server enforces
	const capabilities: { name: string; test: (m: string, p: string) => boolean }[] = [
		{ name: 'Read configurations, versions and audit', test: (m, p) => m === 'GET' && p.startsWith('/configs') },
		{ name: 'Create and edit drafts, import, validate', test: (m, p) => (m === 'PUT' && p.startsWith('/configs/')) || (m === 'POST' && (p === '/configs' || p.endsWith('/validate'))) },
		{ name: 'Approve, return to draft, retire', test: (m, p) => m === 'POST' && /\/(approve|return|retire)$/.test(p) },
		{ name: 'Give people roles', test: (m, p) => m === 'PUT' && p.startsWith('/roles/') },
		{ name: 'Onboard the client', test: (m, p) => m === 'PUT' && p === '/client' },
		{ name: 'Open sandbox books and post', test: (m, p) => m === 'POST' && p.startsWith('/ledger/') }
	];
	const matrix = $derived(
		capabilities.map((c) => {
			const ops = (perms?.operations ?? []).filter((o) => o.access === 'roles' && c.test(o.method, o.path));
			return { name: c.name, roles: new Set(ops.flatMap((o) => o.roles)), ops: ops.length };
		})
	);

	function aiAccept(s: Suggestion) {
		if (s.kind === 'roles' && typeof s.data.user === 'string') {
			const u = s.data.user.trim().toLowerCase();
			const roles = (s.data.roles ?? []).filter((r: string) => allRoles.includes(r));
			if (people.some((p) => p.actor === u)) editing = { ...editing, [u]: roles };
			else {
				newUser = u;
				newRoles = roles;
			}
			tab = 'users';
			return `Opened for your confirmation: ${u} → ${roles.map((r: string) => roleLabels[r]).join(', ')}. Save it with a reason.`;
		}
	}
</script>

<h1 class="ws-h1">Administration</h1>
<p class="ws-lead">Who can do what, client by client. You see only the clients where you are an administrator; the server checks every change again, and every change needs a reason.</p>

{#if !admins.length}
	<div class="card"><p>You are not an administrator of any client.</p></div>
{:else}
	<div class="clients">
		{#each admins as c (c.code)}
			<button class="card ccard" class:on={c.code === sel} onclick={() => go({ page: 'admin', client: c.code })}>
				<b>{c.label}</b>
				<span class="mono small muted">{c.code}</span>
				<span class="row small">
					{#if profiles[c.code] === undefined}<span class="pill">…</span>{:else if profiles[c.code]}<span class="pill {profiles[c.code]?.status === 'ACTIVE' ? 'ok' : 'info'}"
							>{profiles[c.code]?.status === 'ACTIVE' ? 'Active' : 'Onboarding'}</span
						>{:else}<span class="pill warn">Not onboarded</span>{/if}
					<span class="muted">{counts[c.code] ?? '…'} people</span>
				</span>
			</button>
		{/each}
	</div>

	<div class="with-ai">
		<div class="stack">
			<div class="tabs" role="tablist">
				<button role="tab" aria-selected={tab === 'users'} class:on={tab === 'users'} onclick={() => (tab = 'users')}>Users and roles</button>
				<button role="tab" aria-selected={tab === 'perms'} class:on={tab === 'perms'} onclick={() => (tab = 'perms')}>Roles and permissions</button>
				<button role="tab" aria-selected={tab === 'audit'} class:on={tab === 'audit'} onclick={() => (tab = 'audit')}>Audit history</button>
			</div>
			{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
			{#if saved}<p class="notice ok" role="status">{saved}</p>{/if}

			{#if tab === 'users'}
				<section class="card" aria-label="Users and roles">
					<div class="tw">
						<table class="grid">
							<thead><tr><th>User</th>{#each allRoles as r (r)}<th class="c">{roleLabels[r]}</th>{/each}<th></th></tr></thead>
							<tbody>
								{#each people as p (p.actor)}
									{@const cur = editing[p.actor] ?? p.roles.map((r) => r.role)}
									<tr>
										<td><b>{p.actor}</b>{#if p.actor === me}<span class="pill small">you</span>{/if}</td>
										{#each allRoles as r (r)}
											<td class="c"
												><input
													type="checkbox"
													aria-label={`${p.actor} ${roleLabels[r]}`}
													checked={cur.includes(r)}
													disabled={p.actor === me}
													onchange={() => toggle(p.actor, r)}
												/></td
											>
										{/each}
										<td>
											{#if changed(p.actor)}<button class="btn small primary" onclick={() => (asking = { user: p.actor, roles: editing[p.actor] })}>Save…</button>{/if}
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
					{#if people.some((p) => p.actor === me)}<p class="small muted">You cannot change your own roles; another administrator can.</p>{/if}
					<h2 class="sec add">Add a person</h2>
					<div class="row">
						<input class="field" style="min-width:260px" placeholder="name@company.com" bind:value={newUser} aria-label="New user" />
						{#each allRoles as r (r)}
							<label class="small"><input type="checkbox" checked={newRoles.includes(r)} onchange={() => (newRoles = newRoles.includes(r) ? newRoles.filter((x) => x !== r) : [...newRoles, r])} /> {roleLabels[r]}</label>
						{/each}
						<button class="btn primary" disabled={!/^\S+@\S+\.\S+$/.test(newUser.trim()) || !newRoles.length} onclick={() => (asking = { user: newUser.trim().toLowerCase(), roles: newRoles })}>Add…</button>
					</div>
				</section>
			{:else if tab === 'perms'}
				<section class="card" aria-label="Roles and permissions">
					<p class="small muted">What each role may do in this client, read from the operations the server enforces (not a copy).</p>
					<div class="tw">
						<table class="grid">
							<thead><tr><th>Capability</th>{#each allRoles as r (r)}<th class="c">{roleLabels[r]}</th>{/each}</tr></thead>
							<tbody>
								{#each matrix as m (m.name)}
									<tr><td>{m.name}</td>{#each allRoles as r (r)}<td class="c">{#if m.roles.has(r)}<span class="yes">✓</span>{:else}<span class="no">—</span>{/if}</td>{/each}</tr>
								{/each}
								<tr><td>Use the AI assistant</td><td class="c"><span class="yes">✓</span><small> admin</small></td><td class="c"><span class="yes">✓</span></td><td class="c"><span class="yes">✓</span></td><td class="c"><span class="no">—</span></td><td class="c"><span class="no">—</span></td></tr>
							</tbody>
						</table>
					</div>
					<p class="small muted">Nobody approves a version they edited; administrators cannot change their own roles; there is no super-administrator across clients.</p>
					<details class="small"><summary>All {perms?.operations.length ?? 0} API operations</summary>
						<div class="tw"><table class="grid"><tbody>{#each perms?.operations ?? [] as o (o.method + o.path)}<tr><td class="mono">{o.method}</td><td class="mono">{o.path}</td><td>{o.access === 'roles' ? o.roles.map((r) => roleLabels[r] ?? r).join(', ') : o.access}</td></tr>{/each}</tbody></table></div>
					</details>
				</section>
			{:else}
				<section class="card" aria-label="Audit history">
					{#if !audit.length}<p class="muted">No changes yet.</p>{/if}
					<ol class="trail">
						{#each audit as e, i (i)}<li><span class="small muted">{e.at.replace('T', ' ').slice(0, 16)}</span> <b>{e.who}</b> — {e.what}<br /><span class="small muted">“{e.reason}”</span></li>{/each}
					</ol>
				</section>
			{/if}
		</div>
		<AIPanel
			{api}
			area="admin"
			canAct={true}
			accept={aiAccept}
			examples={[
				`Show the users who have access to this client.`,
				'Suggest the roles for the configuration team.',
				'Identify users with conflicting or excessive permissions.',
				'Explain which roles can create, edit, review and approve GL configurations.'
			]}
		/>
	</div>
{/if}

{#if asking}
	<ReasonDialog
		title={`Roles of ${asking.user}`}
		verb="Save roles"
		detail={`${asking.user} will have: ${asking.roles.map((r) => roleLabels[r]).join(', ') || 'no role'} in ${sel}.`}
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = null)}
	/>
{/if}

<style>
	.clients {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 12px;
		margin-bottom: 18px;
	}
	.ccard {
		text-align: left;
		font: inherit;
		color: inherit;
		display: grid;
		gap: 4px;
		cursor: pointer;
	}
	.ccard.on {
		border-color: var(--accent);
		box-shadow: 0 0 0 2px var(--accent-soft);
	}
	.tabs {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--line);
	}
	.tabs button {
		font: inherit;
		font-weight: 600;
		background: none;
		border: 0;
		padding: 8px 14px;
		color: var(--muted);
		border-bottom: 2px solid transparent;
		cursor: pointer;
	}
	.tabs button.on {
		color: var(--accent);
		border-bottom-color: var(--accent);
	}
	.c {
		text-align: center;
	}
	.yes {
		color: var(--ok);
		font-weight: 700;
	}
	.no {
		color: var(--faint);
	}
	.add {
		margin-top: 18px;
	}
	.trail {
		list-style: none;
		padding: 0;
		margin: 0;
		display: grid;
		gap: 10px;
	}
</style>
