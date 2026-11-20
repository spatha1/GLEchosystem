<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { EventView } from '../lib/studio';
	import type { EventTab } from '../lib/route';
	import { glossary, ruleWhen, sideWord } from '../lib/explain';
	import { newRule } from '../lib/content';
	import Formula from './Formula.svelte';
	import EventOverview from './EventOverview.svelte';
	import LineageMap from './LineageMap.svelte';
	import FieldsTable from './FieldsTable.svelte';
	import Simulator from './Simulator.svelte';
	import RulePage from './RulePage.svelte';

	let { ctx }: { ctx: Ctx } = $props();

	let view = $state<EventView | null>(null);
	let failure = $state<ApiError | null>(null);

	$effect(() => {
		const { api, version, route } = ctx;
		ctx.tick;
		api.event(version.configId, version.version, route.event!).then(
			(v) => {
				view = v;
				failure = null;
			},
			(e) => (failure = e)
		);
	});

	const tab = $derived<EventTab>(ctx.route.etab ?? 'overview');
	const base = $derived({ configId: ctx.version.configId, version: ctx.version.version, event: ctx.route.event });
	const known = $derived(view ? glossary(view) : {});
	const errors = $derived(view?.findings.filter((f) => f.severity === 'error').length ?? 0);

	function addRule() {
		ctx.go({ ...base, rule: '+new', edit: true });
	}
</script>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if !view}
	<p class="muted">Loading event {ctx.route.event}…</p>
{:else if ctx.route.rule}
	<RulePage {ctx} {view} />
{:else}
	<section class="evhead">
		<div>
			<div class="row">
				<span class="kind">Business event</span>
				{#if errors}<span class="badge bad">{errors} blocking finding{errors === 1 ? '' : 's'}</span>{/if}
			</div>
			<h2 class="mono">{view.event.code}</h2>
			<p class="purpose">{view.event.description || 'No description configured.'}</p>
		</div>
		<div class="row">
			{#each view.sourceSystems as s (s.code)}<span class="chip">Source: <b>{s.code}</b> <span class="muted">({s.kind.toLowerCase()})</span></span>{/each}
		</div>
	</section>

	<div class="tabs" role="tablist" aria-label="Event views">
		{#each [['overview', 'Definition'], ['lineage', 'Lineage map'], ['rules', 'Posting rules'], ['fields', 'Required source fields'], ['simulate', 'Simulate']] as [k, label] (k)}
			<button role="tab" aria-selected={tab === k} class:active={tab === k} onclick={() => ctx.go({ ...base, etab: k as EventTab })}>
				{label}
				{#if k === 'rules'}<span class="count">{view.rules.length}</span>{/if}
				{#if k === 'fields'}<span class="count">{view.manifest.fields.filter((f) => f.kind === 'SOURCE').length}</span>{/if}
			</button>
		{/each}
	</div>

	{#if tab === 'overview'}
		<EventOverview {ctx} {view} {known} />
	{:else if tab === 'lineage'}
		<LineageMap graph={view.graph} {known} selected={ctx.route.select} onselect={(id) => ctx.go({ ...base, etab: 'lineage', select: id })} />
	{:else if tab === 'rules'}
		<div class="row bar">
			<p class="muted small grow">
				A posting rule turns one {view.event.code} event into journal lines. Each line names an accounting class, a side and an amount formula; the account
				is chosen from the class by assignment rules.
			</p>
			{#if ctx.canEdit}
				<button class="btn primary" onclick={addRule}>Add posting rule</button>
			{:else if ctx.isEditor}
				<button class="btn" onclick={() => ctx.startDraft({ ...base, etab: 'rules' })}>Start a draft to change rules</button>
			{/if}
		</div>
		<div class="stack">
			{#each view.rules as r (r.rule.code)}
				<article class="card rule" class:off={!r.isActive}>
					<div class="row">
						<button class="btn link code mono" onclick={() => ctx.go({ ...base, rule: r.rule.code })}>{r.rule.code}</button>
						{#if r.isActive}<span class="badge ok">Active</span>{:else}<span class="badge off">Deactivated</span>{/if}
						<span class="chip">rank {r.rule.rank}</span>
						<span class="spacer"></span>
						<button class="btn" onclick={() => ctx.go({ ...base, rule: r.rule.code })}>Open rule</button>
					</div>
					{#if r.rule.description}<p class="desc">{r.rule.description}</p>{/if}
					<p class="small muted">{ruleWhen(r, view.event.code)}</p>
					<div class="scroll">
						<table class="data">
							<thead><tr><th>Line</th><th>Side</th><th>Class</th><th>Amount</th><th>Per item of</th><th>Only when</th><th>Entity</th></tr></thead>
							<tbody>
								{#each r.lines as l (l.line.code)}
									<tr>
										<td class="mono">{l.line.code}</td>
										<td class="side-{l.line.side}">{sideWord(l.line.side)}</td>
										<td><span class="mono">{l.line.class}</span> <span class="muted small">{l.class?.name ?? ''}</span></td>
										<td><Formula src={l.line.amount} {known} /></td>
										<td class="mono small">{l.line.expandBy ?? '—'}</td>
										<td>{#if l.line.when}<Formula src={l.line.when} {known} />{:else}<span class="muted">—</span>{/if}</td>
										<td class="mono small">{l.line.entity ?? 'COMPANY'}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</article>
			{:else}
				<div class="card"><p class="muted">No posting rule: {view.event.code} events are refused (MISSING_RULE).</p></div>
			{/each}
		</div>
	{:else if tab === 'fields'}
		<FieldsTable explanation={view.explanation} manifest={view.manifest} {known} graph={view.graph} />
	{:else}
		<Simulator {ctx} fixedEvent={view} />
	{/if}
{/if}

<style>
	.evhead {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: flex-end;
		gap: 12px;
		margin-top: 18px;
	}
	.kind {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--gl-muted);
		font-weight: 600;
	}
	h2 {
		margin: 2px 0;
		font-size: 22px;
	}
	.purpose {
		margin: 0;
		font-size: 15px;
	}
	.bar {
		margin-bottom: 12px;
		align-items: flex-start;
	}
	.grow {
		flex: 1;
		margin: 0;
		min-width: 260px;
	}
	.rule .code {
		font-size: 15px;
		font-weight: 700;
	}
	.rule.off {
		opacity: 0.75;
	}
	.desc {
		margin: 6px 0 2px;
	}
</style>
