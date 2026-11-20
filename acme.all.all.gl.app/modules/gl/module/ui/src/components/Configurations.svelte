<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError, type Client } from '../lib/api';
	import { digest, when } from '../lib/format';
	import type { ActionKind } from '../lib/permissions';
	import type { AuditEntry, ConfigSummary, Finding, Me, Version } from '../lib/types';
	import Findings from './Findings.svelte';
	import ReasonDialog from './ReasonDialog.svelte';
	import StatusChip from './StatusChip.svelte';
	import VersionActions from './VersionActions.svelte';

	let { api, me, onpreview }: { api: Client; me: Me; onpreview: (configId: string, version: number) => void } = $props();

	// Content sections, in the order a reader thinks about them.
	const sections: [string, string][] = [
		['events', 'Events'],
		['sourceAuthority', 'Source authority'],
		['derivedMeasures', 'Derived measures'],
		['dimensions', 'Dimensions'],
		['lookups', 'Lookups'],
		['keyProfiles', 'Business keys'],
		['accountingClasses', 'Accounting classes'],
		['accounts', 'Accounts'],
		['postingRules', 'Posting rules'],
		['accountAssignmentRules', 'Account assignment'],
		['datePolicies', 'Date policies']
	];

	let configs = $state<ConfigSummary[]>([]);
	let filter = $state('');
	let selected = $state('');
	let versions = $state<Version[]>([]);
	let current = $state<Version | null>(null);
	let audit = $state<AuditEntry[]>([]);
	let findings = $state<Finding[] | null>(null);
	let notice = $state<{ kind: 'ok' | 'bad'; text: string } | null>(null);
	let loading = $state(true);
	let dialog = $state<{ kind: 'approve' | 'retire'; v: Version } | null>(null);
	let busy = $state(false);

	const shown = $derived(configs.filter((c) => (c.configId + ' ' + c.title).toLowerCase().includes(filter.trim().toLowerCase())));

	function fail(e: unknown) {
		const a = e instanceof ApiError ? e : null;
		notice = { kind: 'bad', text: a ? `${a.code}: ${a.message}` : String(e) };
	}

	onMount(async () => {
		try {
			configs = await api.configs();
			if (configs.length) await select(configs[0].configId);
		} catch (e) {
			fail(e);
		} finally {
			loading = false;
		}
	});

	async function select(id: string, version?: number) {
		selected = id;
		findings = null;
		const [vs, au] = await Promise.all([api.versions(id), api.audit(id)]);
		versions = [...vs].sort((a, b) => b.version - a.version);
		audit = [...au].reverse();
		const n = version ?? versions[0]?.version;
		current = n ? await api.version(id, n) : null;
		findings = current?.validationReport?.findings ?? null;
	}

	async function open(v: Version) {
		try {
			current = await api.version(v.configId, v.version);
			findings = current.validationReport?.findings ?? null;
		} catch (e) {
			fail(e);
		}
	}

	async function act(kind: ActionKind, v: Version) {
		notice = null;
		if (kind !== 'validate') {
			dialog = { kind, v };
			return;
		}
		busy = true;
		try {
			const r = await api.validate(v.configId, v.version);
			notice = { kind: 'ok', text: `Version ${v.version} is validated. Another approver can now approve it.` };
			await select(v.configId, v.version);
			findings = r.validationReport?.findings ?? [];
		} catch (e) {
			if (e instanceof ApiError && e.code === 'VALIDATION_FAILED') {
				await select(v.configId, v.version); // the failure is audited
				findings = (e.detail as { findings?: Finding[] } | undefined)?.findings ?? [];
				notice = { kind: 'bad', text: `Version ${v.version} has blocking findings and stays a draft.` };
			} else fail(e);
		} finally {
			busy = false;
		}
	}

	async function confirm(reason: string) {
		if (!dialog) return;
		const { kind, v } = dialog;
		busy = true;
		try {
			if (kind === 'approve') await api.approve(v.configId, v.version, reason);
			else await api.retire(v.configId, v.version, reason);
			notice = { kind: 'ok', text: kind === 'approve' ? `Version ${v.version} is approved and in force from ${v.effectiveFrom}.` : `Version ${v.version} is retired.` };
			dialog = null;
			await select(v.configId, v.version);
		} catch (e) {
			dialog = null;
			fail(e);
		} finally {
			busy = false;
		}
	}

	function count(key: string): number {
		const c = current?.content?.[key];
		return Array.isArray(c) ? c.length : 0;
	}
</script>

{#if loading}
	<p class="muted">Loading configurations…</p>
{:else if configs.length === 0}
	<div class="empty">
		<h2>No configurations yet</h2>
		<p>Configurations are created through the API (<code>POST /m/gl/configs</code>). For a local demo, run <code>npm run seed:dev</code>.</p>
	</div>
{:else}
	<div class="layout">
		<aside class="list" aria-label="Configurations">
			<input class="search" placeholder="Filter configurations" bind:value={filter} aria-label="Filter configurations" />
			<ul>
				{#each shown as c (c.configId)}
					<li>
						<button class:active={c.configId === selected} onclick={() => select(c.configId).catch(fail)}>
							<span class="id">{c.configId}</span>
							<span class="title">{c.title || 'Untitled'}</span>
							<span class="meta">v{c.latestVersion} · {c.createdBy}</span>
						</button>
					</li>
				{/each}
			</ul>
		</aside>

		<section class="detail">
			{#if notice}
				<div class="notice {notice.kind}" role="status">{notice.text}</div>
			{/if}

			<div class="card">
				<div class="card-head">
					<h2>{selected}</h2>
					<span class="muted">{versions.length} version{versions.length === 1 ? '' : 's'}</span>
				</div>
				<div class="table-wrap">
					<table>
						<thead>
							<tr>
								<th>Version</th>
								<th>Status</th>
								<th>In force</th>
								<th>Changed by</th>
								<th>Approved by</th>
								<th>Digest</th>
								<th>Actions</th>
							</tr>
						</thead>
						<tbody>
							{#each versions as v (v.version)}
								<tr class:current={current?.version === v.version} onclick={() => open(v)}>
									<td class="num">v{v.version}</td>
									<td><StatusChip status={v.status} /></td>
									<td class="nowrap">{v.effectiveFrom} → {v.effectiveTo ?? 'open'}</td>
									<td>{v.editors.join(', ')}</td>
									<td>{v.approvedBy ?? '—'}</td>
									<td class="mono" title={v.contentDigest}>{digest(v.contentDigest)}</td>
									<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
									<td onclick={(e) => e.stopPropagation()}>
										<VersionActions version={v} {me} onaction={act} onpreview={(x) => onpreview(x.configId, x.version)} />
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>

			{#if current}
				<div class="grid">
					<div class="card">
						<h3>Version {current.version} · what it configures</h3>
						<dl class="counts">
							{#each sections as [key, label] (key)}
								{#if count(key) > 0}
									<div><dt>{label}</dt><dd>{count(key)}</dd></div>
								{/if}
							{/each}
						</dl>
						<p class="muted small">
							Created by {current.createdBy} on {when(current.createdAt)}{#if current.validatedBy}; validated by {current.validatedBy}{/if}{#if current.approvedBy};
								approved by {current.approvedBy} on {when(current.approvedAt)}{/if}.
						</p>
					</div>
					<div class="card">
						<h3>Validation</h3>
						{#if findings}
							<Findings {findings} />
						{:else}
							<p class="muted">Not validated yet. An editor validates a draft; findings appear here.</p>
						{/if}
					</div>
				</div>
			{/if}

			<div class="card">
				<h3>Audit trail</h3>
				{#if audit.length === 0}
					<p class="muted">No entries.</p>
				{:else}
					<ol class="audit">
						{#each audit as a (a.auditId)}
							<li>
								<span class="at">{when(a.at)}</span>
								<span class="action">{a.action.replaceAll('_', ' ')}</span>{#if a.version}<span class="ver">v{a.version}</span>{/if}
								<span class="by">{a.actor}</span>
								{#if a.reason}<span class="reason">“{a.reason}”</span>{/if}
							</li>
						{/each}
					</ol>
				{/if}
			</div>
		</section>
	</div>
{/if}

{#if dialog}
	<ReasonDialog
		title={dialog.kind === 'approve' ? `Approve version ${dialog.v.version}` : `Retire version ${dialog.v.version}`}
		verb={dialog.kind === 'approve' ? 'Approve' : 'Retire'}
		detail={dialog.kind === 'approve'
			? `It will be in force from ${dialog.v.effectiveFrom}. Approved versions cannot be changed; changes become a new version.`
			: 'A retired version is no longer used for new postings.'}
		{busy}
		onsubmit={confirm}
		oncancel={() => (dialog = null)}
	/>
{/if}

<style>
	.layout {
		display: grid;
		grid-template-columns: 260px minmax(0, 1fr);
		gap: 20px;
		align-items: start;
	}
	@media (max-width: 860px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.list {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 10px;
		box-shadow: var(--gl-shadow);
	}
	.search {
		width: 100%;
		box-sizing: border-box;
		font: inherit;
		padding: 7px 10px;
		border: 1px solid var(--gl-border);
		border-radius: 6px;
		background: var(--gl-surface-2);
		color: var(--gl-text);
		margin-bottom: 8px;
	}
	.list ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.list button {
		display: grid;
		width: 100%;
		text-align: left;
		font: inherit;
		padding: 8px 10px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--gl-text);
		cursor: pointer;
	}
	.list button:hover {
		background: var(--gl-surface-2);
	}
	.list button.active {
		background: var(--gl-accent-soft);
	}
	.id {
		font-family: var(--gl-mono);
		font-weight: 600;
		font-size: 13px;
	}
	.title {
		font-size: 13px;
	}
	.meta {
		font-size: 11px;
		color: var(--gl-muted);
	}
	.detail {
		display: grid;
		gap: 16px;
		min-width: 0;
	}
	.card {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 16px;
		box-shadow: var(--gl-shadow);
		min-width: 0;
	}
	.card-head {
		display: flex;
		align-items: baseline;
		gap: 10px;
		margin-bottom: 10px;
	}
	h2 {
		margin: 0;
		font-size: 17px;
		font-family: var(--gl-mono);
	}
	h3 {
		margin: 0 0 10px;
		font-size: 14px;
	}
	.table-wrap {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	th {
		text-align: left;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
		padding: 6px 8px;
		border-bottom: 1px solid var(--gl-border);
		white-space: nowrap;
	}
	td {
		padding: 8px;
		border-bottom: 1px solid var(--gl-border);
		vertical-align: middle;
	}
	tbody tr {
		cursor: pointer;
	}
	tbody tr:hover {
		background: var(--gl-surface-2);
	}
	tr.current {
		background: var(--gl-accent-soft);
	}
	.num {
		font-variant-numeric: tabular-nums;
		font-weight: 600;
	}
	.mono {
		font-family: var(--gl-mono);
		font-size: 12px;
		color: var(--gl-muted);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
		gap: 16px;
	}
	.counts {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 8px;
		margin: 0 0 10px;
	}
	.counts div {
		padding: 8px 10px;
		border-radius: 6px;
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
	}
	dt {
		font-size: 11px;
		color: var(--gl-muted);
	}
	dd {
		margin: 0;
		font-size: 18px;
		font-weight: 650;
		font-variant-numeric: tabular-nums;
	}
	.audit {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 240px;
		overflow: auto;
	}
	.audit li {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 12px;
		padding: 6px 0;
		border-top: 1px solid var(--gl-border);
		font-size: 12.5px;
	}
	.at {
		font-family: var(--gl-mono);
		font-size: 11.5px;
		color: var(--gl-muted);
	}
	.action {
		font-weight: 600;
		text-transform: capitalize;
	}
	.ver {
		font-weight: 600;
		margin-left: -8px;
	}
	.by {
		color: var(--gl-muted);
	}
	.nowrap {
		white-space: nowrap;
	}
	.reason {
		font-style: italic;
	}
	.notice {
		padding: 10px 14px;
		border-radius: var(--gl-radius);
		font-weight: 500;
	}
	.notice.ok {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	.notice.bad {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	.muted {
		color: var(--gl-muted);
	}
	.small {
		font-size: 12px;
		margin: 0;
	}
	.empty {
		background: var(--gl-surface);
		border: 1px dashed var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 24px;
	}
	.empty h2 {
		font-family: inherit;
	}
	code {
		font-family: var(--gl-mono);
		font-size: 12px;
	}
</style>
