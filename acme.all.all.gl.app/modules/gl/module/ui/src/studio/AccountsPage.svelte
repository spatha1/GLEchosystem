<script lang="ts">
	import type { Ctx } from '../lib/ctx';
	import type { Account, AccountingClass, AssignmentRule } from '../lib/studio';

	let { ctx }: { ctx: Ctx } = $props();
	const classes = $derived((ctx.content.accountingClasses ?? []) as AccountingClass[]);
	const accounts = $derived((ctx.content.accounts ?? []) as Account[]);
	const rules = $derived((ctx.content.accountAssignmentRules ?? []) as AssignmentRule[]);
	const normal: Record<string, string> = { ASSET: 'debit', EXPENSE: 'debit', LIABILITY: 'credit', EQUITY: 'credit', INCOME: 'credit' };
</script>

<p class="muted small lead">
	Posting rules name accounting classes (what a line is); assignment rules choose the client's account for each class, by dimension, priority and date.
</p>
<div class="stack">
	<section class="card">
		<h3>Accounting classes <span class="muted">({classes.length})</span></h3>
		<table class="data">
			<thead><tr><th>Class</th><th>Name</th><th>Nature</th><th>Normal side</th><th>Accounts</th></tr></thead>
			<tbody>
				{#each classes as c (c.code)}
					<tr>
						<td class="mono"><b>{c.code}</b></td>
						<td>{c.name}</td>
						<td class="small">{c.nature.toLowerCase()}</td>
						<td class="small">{c.normalSide ? (c.normalSide === 'D' ? 'debit' : 'credit') : normal[c.nature]}</td>
						<td class="mono small">{accounts.filter((a) => a.class === c.code).map((a) => a.code).join(', ') || '—'}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</section>
	<section class="card">
		<h3>Accounts <span class="muted">({accounts.length})</span></h3>
		<div class="scroll">
			<table class="data">
				<thead><tr><th>Account</th><th>Name</th><th>Class</th><th>Ledger</th><th>Entities</th><th>Active</th><th>Requires on every line</th></tr></thead>
				<tbody>
					{#each accounts as a (a.code)}
						<tr>
							<td class="mono"><b>{a.code}</b></td>
							<td>{a.name}</td>
							<td class="mono">{a.class}</td>
							<td class="small">{a.ledger ?? 'PRIMARY'}</td>
							<td class="small mono">{a.entities?.join(', ') ?? 'all'}</td>
							<td class="small">{a.activeFrom ?? '…'} → {a.activeTo ?? 'open'}</td>
							<td class="small">{[a.subledgerKey ? `subledger key ${a.subledgerKey}` : '', ...(a.dimensions ?? []).map((d) => `dimension ${d}`)].filter(Boolean).join(', ') || '—'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>
	<section class="card">
		<h3>Account assignment rules <span class="muted">({rules.length})</span></h3>
		<div class="scroll">
			<table class="data">
				<thead><tr><th>Rule</th><th>Class</th><th>When</th><th>Account</th><th>Priority</th><th>Effective</th></tr></thead>
				<tbody>
					{#each rules as r (r.code)}
						<tr>
							<td class="mono"><b>{r.code}</b></td>
							<td class="mono">{r.class}</td>
							<td class="small">{Object.entries(r.match ?? {}).map(([k, v]) => `${k} = ${v === '*' ? 'any' : v}`).join(' and ') || 'always (fallback)'}</td>
							<td class="mono">{r.account} <span class="small muted">{accounts.find((a) => a.code === r.account)?.name ?? ''}</span></td>
							<td class="num">{r.priority ?? 0}</td>
							<td class="small">{r.effectiveFrom ?? '…'} → {r.effectiveTo ?? 'open'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="small muted">Precedence: lower priority first, then more exact matches, then more wildcards, then the later effective date. A tie is refused, never guessed.</p>
	</section>
</div>

<style>
	.lead {
		margin: 0 0 12px;
	}
</style>
