<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { AuditEntryFull } from '../lib/studio';
	import type { Finding } from '../lib/types';
	import { digest, when } from '../lib/format';
	import Findings from '../components/Findings.svelte';

	let { ctx, findings }: { ctx: Ctx; findings: Finding[] } = $props();
	let entries = $state<AuditEntryFull[] | null>(null);
	let failure = $state<ApiError | null>(null);

	$effect(() => {
		ctx.tick;
		ctx.api.audit(ctx.version.configId).then(
			(es) => (entries = es),
			(e) => (failure = e)
		);
	});

	const action: Record<string, string> = {
		create_config: 'Created the configuration',
		create_version: 'Created the version',
		update_draft: 'Saved changes',
		validate: 'Validated',
		validation_failed: 'Validation failed',
		approve: 'Approved',
		retire: 'Retired'
	};
</script>

<div class="grid2">
	<section class="card">
		<h3>Validation findings for v{ctx.version.version}</h3>
		<p class="small muted">Recomputed from the version's content now. Errors block validation; warnings do not.</p>
		{#if findings.length}<Findings {findings} />{:else}<p class="notice ok small">No findings: the version is consistent.</p>{/if}
	</section>
	<section class="card">
		<h3>Maker-checker</h3>
		<dl class="kv">
			<dt>Created by</dt><dd>{ctx.version.createdBy} · {when(ctx.version.createdAt)}</dd>
			<dt>Changed by</dt><dd>{ctx.version.editors.join(', ')}</dd>
			<dt>Validated by</dt><dd>{ctx.version.validatedBy ?? '—'} {when(ctx.version.validatedAt)}</dd>
			<dt>Approved by</dt><dd>{ctx.version.approvedBy ?? '—'} {when(ctx.version.approvedAt)}</dd>
		</dl>
		<p class="small muted">An approver must not be one of the editors; the database refuses it otherwise.</p>
	</section>
</div>

<section class="card">
	<h3>Audit trail of {ctx.version.configId}</h3>
	{#if failure}
		<div class="notice bad">{failure.code}: {failure.message}</div>
	{:else if !entries}
		<p class="muted">Loading…</p>
	{:else}
		<ol class="trail">
			{#each [...entries].reverse() as e (e.auditId)}
				<li class:this={e.version === ctx.version.version}>
					<div class="row">
						<span class="mono ver">v{e.version ?? '—'}</span>
						<b>{action[e.action] ?? e.action}</b>
						<span class="muted small">by {e.actor} · {when(e.at)}</span>
						{#if e.fromStatus || e.toStatus}<span class="chip small">{e.fromStatus ?? '—'} → {e.toStatus ?? '—'}</span>{/if}
						{#if e.contentDigest}<span class="mono small muted">{digest(e.contentDigest)}</span>{/if}
					</div>
					{#if e.reason}<div class="reason">“{e.reason}”</div>{/if}
					{#if e.detail?.changes?.length}
						<ul class="changes small mono">{#each e.detail.changes as c, i (i)}<li>{c}</li>{/each}</ul>
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
</section>

<style>
	.trail {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 0;
	}
	.trail li {
		padding: 8px 0 8px 12px;
		border-left: 3px solid var(--gl-border);
	}
	.trail li.this {
		border-left-color: var(--gl-accent);
	}
	.ver {
		font-weight: 700;
		color: var(--gl-accent);
	}
	.reason {
		margin-top: 2px;
		font-style: italic;
	}
	.changes {
		margin: 4px 0 0;
		padding-left: 18px;
		color: var(--gl-muted);
	}
</style>
