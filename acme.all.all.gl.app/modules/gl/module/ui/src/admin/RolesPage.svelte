<script lang="ts">
	// ALTB-08 — the client's people and their roles, managed by a GL administrator in the tool.
	// The client (tenant) and its first administrator come from the platform; everyone else is
	// added here. Every change needs a reason and is kept in the role history; an administrator
	// cannot change their own roles (another administrator must).
	import type { ApiError, Client } from '../lib/api';
	import type { Me, RoleAssignment, RoleAuditEntry } from '../lib/types';
	import { when } from '../lib/format';

	let { api, me }: { api: Client; me: Me } = $props();

	const ROLES: { code: string; name: string; can: string }[] = [
		{ code: 'CONFIG_EDITOR', name: 'Business analyst (editor)', can: 'creates and edits configuration drafts, simulates, validates' },
		{ code: 'CONFIG_APPROVER', name: 'Approver', can: 'approves and retires versions someone else edited' },
		{ code: 'OPERATOR', name: 'Operator', can: 'opens sandbox books and posts with approved versions' },
		{ code: 'VIEWER', name: 'Viewer', can: 'reads everything, changes nothing' },
		{ code: 'GL_ADMIN', name: 'GL administrator', can: 'manages people and roles in this client' }
	];
	const nameOf = (r: string) => ROLES.find((x) => x.code === r)?.name ?? r;

	let people = $state<RoleAssignment[] | null>(null);
	let history = $state<RoleAuditEntry[]>([]);
	let failure = $state('');
	let editing = $state<{ actor: string; roles: string[]; isNew: boolean } | null>(null);
	let reason = $state('');
	let error = $state('');
	let busy = $state(false);

	async function load() {
		failure = '';
		try {
			[people, history] = await Promise.all([api.roles(), api.roleAudit()]);
		} catch (e) {
			const err = e as ApiError;
			failure = `${err.code}: ${err.message}`;
		}
	}
	$effect(() => {
		load();
	});

	const validEmail = (s: string) => /^[^\s@]+@[^\s@]+$/.test(s.trim());
	const canSave = $derived(!!editing && validEmail(editing.actor) && reason.trim().length > 0 && !busy);

	function start(p?: RoleAssignment) {
		error = '';
		reason = '';
		editing = p ? { actor: p.actor, roles: p.roles.map((r) => r.role), isNew: false } : { actor: '', roles: [], isNew: true };
	}
	function toggle(code: string) {
		if (!editing) return;
		editing.roles = editing.roles.includes(code) ? editing.roles.filter((r) => r !== code) : [...editing.roles, code];
	}
	async function save() {
		if (!editing) return;
		busy = true;
		error = '';
		try {
			await api.setRoles(editing.actor.trim().toLowerCase(), editing.roles, reason.trim());
			editing = null;
			await load();
		} catch (e) {
			const err = e as ApiError;
			error = `${err.code}: ${err.message}`;
		} finally {
			busy = false;
		}
	}
</script>

<div class="roles">
	<section class="card">
		<div class="row">
			<h3 class="grow">People and roles <span class="muted">· client {me.tenantId}</span></h3>
			{#if !editing}<button class="btn primary" onclick={() => start()}>+ Add a person</button>{/if}
		</div>
		<p class="small muted">
			The client and its first administrator come from the platform. Here an administrator gives each person the roles they need. Every change needs a reason
			and is kept below. You cannot change your own roles: another administrator must.
		</p>
		{#if failure}<div class="notice bad" role="alert">{failure}</div>{/if}

		{#if editing}
			<form
				class="edit"
				aria-label={editing.isNew ? 'Add a person' : `Roles of ${editing.actor}`}
				onsubmit={(e) => {
					e.preventDefault();
					save();
				}}
			>
				<label class="lbl"
					>Person (sign-in e-mail)
					<input class="field" bind:value={editing.actor} disabled={!editing.isNew} placeholder="name@company.com" />
				</label>
				<fieldset>
					<legend>Roles</legend>
					{#each ROLES as r (r.code)}
						<label class="opt">
							<input type="checkbox" checked={editing.roles.includes(r.code)} onchange={() => toggle(r.code)} />
							<span><b>{r.name}</b> <span class="mono small muted">{r.code}</span><br /><span class="small muted">{r.can}</span></span>
						</label>
					{/each}
				</fieldset>
				<label class="lbl">Reason (kept in the role history) <textarea class="field" rows="2" bind:value={reason} placeholder="Why this person needs these roles"></textarea></label>
				{#if editing.roles.includes('CONFIG_EDITOR') && editing.roles.includes('CONFIG_APPROVER')}
					<p class="small muted">Editor and approver together: allowed, but this person can never approve a version they edited (maker-checker).</p>
				{/if}
				<div class="row">
					<button class="btn primary" type="submit" disabled={!canSave}>{editing.roles.length ? 'Save roles' : 'Remove all roles'}</button>
					<button class="btn" type="button" onclick={() => (editing = null)}>Cancel</button>
				</div>
				{#if error}<div class="notice bad" role="alert">{error}</div>{/if}
			</form>
		{/if}

		{#if people?.length}
			<table class="data">
				<thead><tr><th>Person</th><th>Roles</th><th>Granted</th><th></th></tr></thead>
				<tbody>
					{#each people as p (p.actor)}
						<tr>
							<td>{p.actor}{#if p.actor === me.actor}&nbsp;<span class="badge info">you</span>{/if}</td>
							<td>{#each p.roles as r (r.role)}<span class="chip" title={r.role}>{nameOf(r.role)}</span>{/each}</td>
							<td class="small muted">{p.roles.map((r) => `${r.grantedBy}, ${when(r.grantedAt)}`).filter((x, i, a) => a.indexOf(x) === i).join('; ')}</td>
							<td><button class="btn link" onclick={() => start(p)} disabled={!!editing}>Change</button></td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else if people}
			<p class="small muted">Nobody has a role yet.</p>
		{/if}
	</section>

	<section class="card" aria-label="Role history">
		<h3>Role history</h3>
		{#if history.length}
			<table class="data">
				<thead><tr><th>When</th><th>By</th><th>Person</th><th>Granted</th><th>Revoked</th><th>Reason</th></tr></thead>
				<tbody>
					{#each [...history].reverse() as h (h.auditId)}
						<tr>
							<td class="small">{when(h.at)}</td>
							<td class="small">{h.actor}</td>
							<td>{h.subject}</td>
							<td class="small">{(h.granted ?? []).map(nameOf).join(', ') || '—'}</td>
							<td class="small">{(h.revoked ?? []).map(nameOf).join(', ') || '—'}</td>
							<td class="small">{h.reason}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="small muted">No changes yet.</p>
		{/if}
	</section>
</div>

<style>
	.roles {
		display: grid;
		gap: 14px;
	}
	.grow {
		flex: 1;
	}
	.edit {
		display: grid;
		gap: 10px;
		margin: 10px 0 14px;
		max-width: 720px;
	}
	fieldset {
		border: 1px solid var(--gl-border);
		border-radius: 8px;
		padding: 8px 10px 10px;
		display: grid;
		gap: 8px;
		margin: 0;
	}
	legend {
		font-size: 12px;
		font-weight: 650;
		padding: 0 4px;
	}
	.opt {
		display: flex;
		gap: 8px;
		align-items: flex-start;
	}
	.chip + .chip {
		margin-left: 4px;
	}
	.roles :global(table.data) {
		width: 100%;
	}
</style>
