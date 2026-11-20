<script lang="ts">
	// One journal leg traced back: source record → source fields (with values) → calculation
	// → posting rule line → account assignment → journal leg. Everything shown comes from the
	// plan the server computed and the event's configuration view.
	import type { Ctx } from '../lib/ctx';
	import type { Account, EventInput, EventView, Plan } from '../lib/studio';
	import type { PlannedLine } from '../lib/types';
	import { amount } from '../lib/format';
	import { sideWord } from '../lib/explain';
	import Formula from './Formula.svelte';

	let {
		ctx,
		view,
		plan,
		line,
		input,
		scenario,
		known
	}: { ctx: Ctx; view: EventView; plan: Plan; line: PlannedLine; input: EventInput; scenario?: string; known: Record<string, string> } = $props();

	const base = $derived({ configId: ctx.version.configId, version: ctx.version.version, event: view.event.code });
	const rv = $derived(view.rules.find((r) => r.rule.code === plan.rule));
	const lv = $derived(rv?.lines.find((l) => l.line.code === line.template));
	const accounts = $derived((ctx.content.accounts ?? []) as Account[]);
	const acct = $derived(accounts.find((a) => a.code === line.account));
	const derivedRead = $derived(plan.derived.filter((d) => (line.trace?.reads ?? []).some((r) => r.name === d.code)));
	// Raw values: what the line read directly that is not derived, plus what its formulas read.
	const rawReads = $derived.by(() => {
		const seen = new Map<string, string>();
		for (const r of line.trace?.reads ?? []) if (!plan.derived.some((d) => d.code === r.name)) seen.set(r.name, r.value);
		for (const d of derivedRead) for (const r of d.trace?.reads ?? []) seen.set(r.name, r.value);
		return [...seen.entries()];
	});
	const fieldInfo = (code: string) => view.explanation.fields.find((f) => f.fieldCode === code);
	const entityDim = $derived(plan.dimensions.find((d) => d.dimension === (lv?.line.entity?.trim() || 'COMPANY')));
	// The raw value the legal entity comes from: the dimension itself, or a lookup's input.
	const entitySource = $derived.by(() => {
		if (!entityDim) return null;
		if (entityDim.via === 'LOOKUP') {
			const input = view.lookups.find((l) => l.code === entityDim.lookup)?.input ?? '';
			return { name: input, value: entityDim.key ?? '', info: fieldInfo(input) };
		}
		return { name: entityDim.dimension, value: entityDim.value, info: fieldInfo(entityDim.dimension) };
	});
	const flipped = $derived(lv && lv.line.side !== line.side);
</script>

<section class="card lineage">
	<div class="row">
		<h3 class="grow">Where line {line.lineNo} comes from</h3>
		<span class="badge {line.side === 'D' ? 'info' : 'warn'}">{sideWord(line.side)} {amount(line.amount)} {line.currency}</span>
	</div>
	<ol class="steps">
		<li>
			<div class="st">Source record</div>
			<div>
				<b>{input.sourceSystem ?? 'unknown source'}</b> record <span class="mono">{input.sourceEventId ?? '—'}</span> version {input.sourceEventVersion ?? 1}
				— a <button class="btn link mono" onclick={() => ctx.go(base)}>{view.event.code}</button> event
				{#if line.item}, item {line.item} of its list{/if}
			</div>
			<div class="small muted">
				{scenario ? `Scenario ${scenario} of this configuration version (synthetic input).` : 'Entered in the simulator (synthetic input).'} Collected source records
				arrive once data collection (US-017) is built.
			</div>
		</li>
		<li>
			<div class="st">Source fields read</div>
			<table class="data small">
				<thead><tr><th>Field</th><th>Value</th><th>Canonical event field</th><th>From</th><th>Mapping</th></tr></thead>
				<tbody>
					{#each rawReads as [name, value] (name)}
						{@const f = fieldInfo(name.includes('.') ? name.split('.')[1] : name)}
						<tr>
							<td class="mono"><b>{name}</b></td>
							<td class="mono">{value}</td>
							<td class="mono">{f?.path ?? '—'}</td>
							<td>{f?.sourceSystems?.join(', ') ?? '—'}</td>
							<td class="muted">not mapped yet</td>
						</tr>
					{/each}
					{#if entitySource && !rawReads.some(([n]) => n === entitySource.name)}
						<tr>
							<td class="mono"><b>{entitySource.name}</b> <span class="muted small">(legal entity)</span></td>
							<td class="mono">{entitySource.value}</td>
							<td class="mono">{entitySource.info?.path ?? '—'}</td>
							<td>{entitySource.info?.sourceSystems?.join(', ') ?? '—'}</td>
							<td class="muted">not mapped yet</td>
						</tr>
					{/if}
				</tbody>
			</table>
		</li>
		<li>
			<div class="st">Calculation</div>
			{#each derivedRead as d (d.code)}
				<div>
					<span class="mono"><b>{d.code}</b></span> = <Formula src={d.formula} {known} /> = {(d.trace?.reads ?? []).map((r) => r.value).join(' , ')} →
					<b class="mono">{d.value}</b>
				</div>
			{/each}
			<div>
				Line amount <Formula src={lv?.line.amount ?? '?'} {known} /> = <b class="mono">{amount(line.amount)} {line.currency}</b>
				{#if flipped}<span class="badge warn">negative: posted on the other side</span>{/if}
			</div>
			{#if entityDim && entityDim.via === 'LOOKUP'}
				<div>Legal entity <span class="mono">{entityDim.dimension}</span> = lookup <span class="mono">{entityDim.lookup}</span>({entityDim.key}) → <b class="mono">{entityDim.value}</b></div>
			{/if}
		</li>
		<li>
			<div class="st">Posting rule</div>
			<div>
				<button class="btn link mono" onclick={() => ctx.go({ ...base, rule: plan.rule })}>{plan.rule}</button>
				(rank {plan.rank}) — {plan.considered.find((c) => c.rule === plan.rule)?.reason}
			</div>
			<div>
				line <span class="mono"><b>{line.template}</b></span>: {sideWord(lv?.line.side ?? line.side)} <span class="mono">{line.class}</span>
				{lv?.class ? `(${lv.class.name})` : ''}{lv?.line.when ? `, only when ${lv.line.when} (true)` : ''}{lv?.line.expandBy ? `, one per ${lv.line.expandBy} item` : ''}
			</div>
			<div class="small muted">{line.explanation}</div>
		</li>
		<li>
			<div class="st">Account assignment</div>
			{#if line.assignment?.rule}
				<div>
					<span class="mono"><b>{line.assignment.rule}</b></span> chose <b class="mono">{line.assignment.account}</b>
					{acct ? acct.name : ''} — {(line.assignment.why ?? []).join('; ') || 'no conditions'}
				</div>
				<div class="small muted">
					Considered: {(line.assignment.considered ?? []).map((c) => `${c.rule} (${c.matched ? 'matched' : c.reason})`).join(' · ')}
				</div>
			{:else}
				<div class="notice bad small">{line.problem ?? 'No account was assigned.'}</div>
			{/if}
		</li>
		<li>
			<div class="st">Journal leg</div>
			<div>
				<b>{line.entity}</b> · account <b class="mono">{line.account ?? '—'}</b> · <span class="side-{line.side}">{sideWord(line.side)} {amount(line.amount)} {line.currency}</span>
				· booked {plan.bookDate ?? '—'}{plan.period ? `, period ${plan.period.accountingPeriod}` : ''}
			</div>
			<div class="small muted">
				Keys: {Object.entries(line.keys)
					.map(([k, v]) => `${k} ${v}`)
					.join(' · ') || '—'} · configuration {plan.config.configId} v{plan.config.configVersion}
			</div>
			{#if line.problem}<div class="notice bad small" role="alert">{line.problem}</div>{/if}
		</li>
	</ol>
</section>

<style>
	.lineage {
		border-color: var(--gl-accent);
	}
	.grow {
		flex: 1;
	}
	.steps {
		list-style: none;
		margin: 6px 0 0;
		padding: 0;
		counter-reset: st;
		display: grid;
		gap: 0;
	}
	.steps > li {
		position: relative;
		padding: 0 0 14px 34px;
		display: grid;
		gap: 3px;
	}
	.steps > li::before {
		counter-increment: st;
		content: counter(st);
		position: absolute;
		left: 0;
		top: 0;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		display: grid;
		place-items: center;
		font-size: 11px;
		font-weight: 700;
	}
	.steps > li:not(:last-child)::after {
		content: '';
		position: absolute;
		left: 10px;
		top: 24px;
		bottom: 2px;
		width: 2px;
		background: var(--gl-border-strong);
	}
	.st {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--gl-muted);
		font-weight: 700;
	}
	table {
		margin-top: 2px;
	}
</style>
