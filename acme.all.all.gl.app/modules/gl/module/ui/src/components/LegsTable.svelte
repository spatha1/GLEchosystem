<script lang="ts">
	import { amount, keyList } from '../lib/format';
	import type { PlannedLine } from '../lib/types';

	let { lines }: { lines: PlannedLine[] } = $props();
	let open = $state<Record<number, boolean>>({});
	const toggle = (n: number) => (open = { ...open, [n]: !open[n] });
</script>

<div class="wrap">
	<table>
		<thead>
			<tr>
				<th class="n">#</th>
				<th>Class</th>
				<th>Account</th>
				<th>Entity</th>
				<th class="amt">Debit</th>
				<th class="amt">Credit</th>
				<th>Keys</th>
				<th><span class="sr">Details</span></th>
			</tr>
		</thead>
		<tbody>
			{#each lines as l (l.lineNo)}
				<tr class:problem={!!l.problem}>
					<td class="n">{l.lineNo}</td>
					<td><span class="class">{l.class}</span></td>
					<td class="acct">{l.account ?? '—'}</td>
					<td>{l.entity}</td>
					<td class="amt" data-side="D">{l.side === 'D' ? `${amount(l.amount)} ${l.currency}` : ''}</td>
					<td class="amt" data-side="C">{l.side === 'C' ? `${amount(l.amount)} ${l.currency}` : ''}</td>
					<td class="keys">{keyList(l.keys)}</td>
					<td>
						<button class="more" aria-expanded={!!open[l.lineNo]} aria-label="Why line {l.lineNo}" onclick={() => toggle(l.lineNo)}>
							{open[l.lineNo] ? 'Hide' : 'Why'}
						</button>
					</td>
				</tr>
				{#if l.problem}
					<tr class="problem-row"><td></td><td colspan="7"><div role="alert">{l.problem}</div></td></tr>
				{/if}
				{#if open[l.lineNo]}
					<tr class="why-row">
						<td></td>
						<td colspan="7">
							<div class="why">
								<div>
									<h4>Amount</h4>
									<p>{l.explanation}</p>
									{#if l.trace?.reads?.length}
										<p class="reads">Read: {l.trace.reads.map((r) => `${r.name} = ${r.value}`).join(', ')}</p>
									{/if}
									<p class="muted">Template {l.template}{l.item ? `, item ${l.item}` : ''}</p>
								</div>
								{#if l.assignment}
									<div>
										<h4>Account</h4>
										{#if l.assignment.rule}
											<p><strong>{l.assignment.account}</strong> by rule <code>{l.assignment.rule}</code> (configuration {l.assignment.config.configId} v{l.assignment.config.configVersion})</p>
											<ul>
												{#each l.assignment.why as w, i (i)}<li>{w}</li>{/each}
											</ul>
										{/if}
										{#if l.assignment.considered.length}
											<p class="muted">Rules considered:</p>
											<ul class="considered">
												{#each l.assignment.considered as c (c.rule)}
													<li class:matched={c.matched}><code>{c.rule}</code> → {c.account}: {c.reason}</li>
												{/each}
											</ul>
										{/if}
									</div>
								{/if}
							</div>
						</td>
					</tr>
				{/if}
			{/each}
		</tbody>
	</table>
</div>

<style>
	.wrap {
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
		vertical-align: top;
	}
	.n {
		width: 28px;
		color: var(--gl-muted);
		font-variant-numeric: tabular-nums;
	}
	.amt {
		text-align: right;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
		font-family: var(--gl-mono);
		font-size: 12.5px;
	}
	.class {
		font-family: var(--gl-mono);
		font-weight: 600;
		padding: 1px 6px;
		border-radius: 4px;
		background: var(--gl-accent-soft);
		color: var(--gl-accent);
	}
	.acct {
		font-family: var(--gl-mono);
		font-weight: 600;
	}
	.keys {
		font-size: 12px;
		color: var(--gl-muted);
	}
	.more {
		font: inherit;
		font-size: 12px;
		padding: 2px 8px;
		border: 1px solid var(--gl-border);
		border-radius: 6px;
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
	}
	tr.problem td {
		background: var(--gl-bad-soft);
	}
	.problem-row td {
		color: var(--gl-bad);
		background: var(--gl-bad-soft);
		font-size: 12.5px;
	}
	.why-row td {
		background: var(--gl-surface-2);
	}
	.why {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: 16px;
		font-size: 12.5px;
	}
	h4 {
		margin: 0 0 4px;
		font-size: 12px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
	}
	p {
		margin: 0 0 4px;
	}
	ul {
		margin: 0 0 4px;
		padding-left: 18px;
	}
	.considered li {
		color: var(--gl-muted);
	}
	.considered li.matched {
		color: var(--gl-text);
	}
	.reads {
		font-family: var(--gl-mono);
		font-size: 11.5px;
	}
	.muted {
		color: var(--gl-muted);
	}
	code {
		font-family: var(--gl-mono);
		font-size: 11.5px;
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
	}
</style>
