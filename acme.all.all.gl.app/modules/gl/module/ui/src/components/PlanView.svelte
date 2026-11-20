<script lang="ts">
	import { amount } from '../lib/format';
	import type { PostingPlan } from '../lib/types';
	import LegsTable from './LegsTable.svelte';

	let { plan }: { plan: PostingPlan } = $props();
	const refused = $derived(plan.lines.filter((l) => l.problem).length);
	const postable = $derived(plan.balanced && refused === 0);
</script>

<div class="summary">
	<div><span class="label">Rule</span><span class="value mono">{plan.rule}</span><span class="sub">rank {plan.rank}</span></div>
	<div><span class="label">Event</span><span class="value mono">{plan.eventType}</span></div>
	<div><span class="label">Book date</span><span class="value">{plan.bookDate ?? '—'}</span></div>
	<div><span class="label">Configuration</span><span class="value mono">{plan.config.configId} v{plan.config.configVersion}</span></div>
	<div>
		<span class="label">Balance</span>
		<span class="badge {plan.balanced ? 'ok' : 'bad'}">{plan.balanced ? 'Balanced' : 'Not balanced'}</span>
	</div>
	<div>
		<span class="label">Posting</span>
		<span class="badge {postable ? 'ok' : 'bad'}">{postable ? 'Can post' : 'Refused'}</span>
		{#if refused}<span class="sub">{refused} leg{refused === 1 ? '' : 's'} without a usable account</span>{/if}
	</div>
</div>

<div class="card">
	<h3>Journal legs <span class="muted">({plan.lines.length})</span></h3>
	<LegsTable lines={plan.lines} />
</div>

<div class="cols">
	<div class="card">
		<h3>Balance by entity and currency</h3>
		<table class="totals">
			<thead><tr><th>Entity</th><th>Currency</th><th class="amt">Debit</th><th class="amt">Credit</th><th></th></tr></thead>
			<tbody>
				{#each plan.totals as t (t.entity + t.currency)}
					<tr>
						<td class="mono">{t.entity}</td>
						<td>{t.currency}</td>
						<td class="amt">{amount(t.debit)}</td>
						<td class="amt">{amount(t.credit)}</td>
						<td><span class="badge {t.balanced ? 'ok' : 'bad'}">{t.balanced ? '✓ balanced' : 'off'}</span></td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<div class="card">
		<h3>Why this rule</h3>
		<ul class="plain">
			{#each plan.considered as c (c.rule)}
				<li class:matched={c.matched}>
					<code>{c.rule}</code> <span class="muted">rank {c.rank}</span>{#if c.filter} <span class="muted">· {c.filter}</span>{/if} — {c.reason}
				</li>
			{/each}
		</ul>
		{#if plan.skipped.length}
			<h3 class="gap">Skipped legs</h3>
			<ul class="plain">
				{#each plan.skipped as s, i (i)}<li class="muted">{s}</li>{/each}
			</ul>
		{/if}
	</div>
</div>

<style>
	.summary {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 10px;
		margin-bottom: 16px;
	}
	.summary > div {
		display: grid;
		gap: 2px;
		padding: 10px 12px;
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
	}
	.label {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
	}
	.value {
		font-weight: 650;
	}
	.sub {
		font-size: 11px;
		color: var(--gl-muted);
	}
	.badge {
		justify-self: start;
		padding: 2px 9px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 600;
	}
	.badge.ok {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	.badge.bad {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	.card {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 16px;
		box-shadow: var(--gl-shadow);
		margin-bottom: 16px;
		min-width: 0;
	}
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
		gap: 16px;
	}
	.cols .card {
		margin-bottom: 0;
	}
	h3 {
		margin: 0 0 10px;
		font-size: 14px;
	}
	h3.gap {
		margin-top: 14px;
	}
	.totals {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	.totals th {
		text-align: left;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
		padding: 4px 6px;
		border-bottom: 1px solid var(--gl-border);
	}
	.totals td {
		padding: 6px;
		border-bottom: 1px solid var(--gl-border);
	}
	.amt {
		text-align: right !important;
		font-family: var(--gl-mono);
		font-variant-numeric: tabular-nums;
	}
	.mono {
		font-family: var(--gl-mono);
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		font-size: 12.5px;
	}
	.plain li {
		padding: 4px 0;
		border-top: 1px solid var(--gl-border);
		color: var(--gl-muted);
	}
	.plain li.matched {
		color: var(--gl-text);
	}
	.muted {
		color: var(--gl-muted);
	}
	code {
		font-family: var(--gl-mono);
		font-size: 12px;
		font-weight: 600;
	}
</style>
