<script lang="ts">
	// ALTB-04 — lifecycle preview: a synthetic policy's events, in the order chosen, run through
	// the same posting engine as the one-event simulation, against a temporary ledger, so a later
	// event can read what earlier ones posted. The ledger is thrown away: nothing is stored or
	// posted, and nothing is chosen until the user adds a step.
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { Account, LifecycleResult, LifecycleStep, Scenario } from '../lib/studio';
	import { amount } from '../lib/format';
	import { movementWord } from '../lib/explain';

	let { ctx }: { ctx: Ctx } = $props();

	const scenarios = $derived((ctx.content.scenarios ?? []) as Scenario[]);
	const accounts = $derived((ctx.content.accounts ?? []) as Account[]);
	const acctName = (code?: string) => accounts.find((a) => a.code === code)?.name ?? '';

	let steps = $state<string[]>([]);
	let adding = $state('');
	let result = $state<LifecycleResult | null>(null);
	let failure = $state('');
	let running = $state(false);
	let shown = $state<number | null>(null);

	const shownStep = $derived<LifecycleStep | undefined>(result?.steps.find((s) => s.step === shown));
	const eventOf = (code: string) => scenarios.find((s) => s.code === code)?.eventType ?? '';

	function add() {
		if (!adding) return;
		steps = [...steps, adding];
		adding = '';
		result = null;
	}
	function move(i: number, by: number) {
		const j = i + by;
		if (j < 0 || j >= steps.length) return;
		const next = [...steps];
		[next[i], next[j]] = [next[j], next[i]];
		steps = next;
		result = null;
	}
	function remove(i: number) {
		steps = steps.filter((_, j) => j !== i);
		result = null;
	}

	async function run() {
		running = true;
		failure = '';
		result = null;
		try {
			result = await ctx.api.lifecycle(ctx.version.configId, ctx.version.version, steps.map((scenario) => ({ scenario })));
			const bad = result.steps.find((s) => s.error);
			shown = bad?.step ?? result.steps.at(-1)?.step ?? null;
		} catch (e) {
			const err = e as ApiError;
			failure = `${err.code}: ${err.message}`;
		} finally {
			running = false;
		}
	}
</script>

<section class="card life" aria-label="Lifecycle preview">
	<div class="row">
		<h3 class="grow">Lifecycle preview <span class="badge info">Preview · not posted</span></h3>
	</div>
	<p class="small muted">
		Runs scenarios of v{ctx.version.version} in the order below through the same engine as the simulation above, against a temporary ledger, so a later
		event reads what earlier ones posted (for example a receipt against an invoice). The ledger is thrown away with the result: nothing is stored or
		posted.
	</p>

	<div class="row pick">
		<label class="lbl"
			>Add a scenario
			<select class="field" bind:value={adding}>
				<option value="">Choose…</option>
				{#each scenarios as s (s.code)}<option value={s.code}>{s.code} · {s.eventType}{s.description ? ` — ${s.description}` : ''}</option>{/each}
			</select>
		</label>
		<button class="btn" onclick={add} disabled={!adding}>Add step</button>
		<span class="spacer"></span>
		<button class="btn primary" onclick={run} disabled={!steps.length || running}>{running ? 'Running…' : 'Run lifecycle'}</button>
	</div>

	{#if steps.length}
		<ol class="steps">
			{#each steps as code, i (i)}
				<li>
					<span class="mono"><b>{code}</b></span> <span class="small muted">{eventOf(code)}</span>
					<span class="spacer"></span>
					<button class="btn link" onclick={() => move(i, -1)} disabled={i === 0} aria-label="Move {code} up">↑</button>
					<button class="btn link" onclick={() => move(i, 1)} disabled={i === steps.length - 1} aria-label="Move {code} down">↓</button>
					<button class="btn link" onclick={() => remove(i)} aria-label="Remove {code}">Remove</button>
				</li>
			{/each}
		</ol>
	{:else}
		<p class="small muted">No steps yet: add the scenarios of one policy in the order they happen.</p>
	{/if}

	{#if failure}<div class="notice bad" role="alert">{failure}</div>{/if}

	{#if result}
		<div class="sum">
			<span class="badge {result.complete ? 'ok' : 'bad'}">{result.complete ? 'Every step can post' : 'Stopped'}</span>
			<span class="small">{result.steps.length} step{result.steps.length === 1 ? '' : 's'} · {result.lines} lines · {result.entries} journal entries</span>
			<span class="badge info">Not posted</span>
		</div>
		{#if result.stopped}<div class="notice bad" role="alert">Stopped at {result.stopped}. Later steps were not run: they may depend on it.</div>{/if}

		<table class="data">
			<thead><tr><th>#</th><th>Scenario</th><th>Event</th><th>Rule</th><th>Book date</th><th>Result</th><th></th></tr></thead>
			<tbody>
				{#each result.steps as s (s.step)}
					<tr class:on={shown === s.step}>
						<td>{s.step}</td>
						<td class="mono"><b>{s.code}</b></td>
						<td class="mono">{s.plan?.eventType ?? eventOf(s.code ?? '')}</td>
						<td class="mono">{s.plan?.rule || '—'}</td>
						<td>{s.plan?.bookDate ?? '—'}</td>
						<td>
							{#if s.error}<span class="badge bad">{s.error.code}</span>{:else}<span class="badge ok">{s.plan?.lines.length} lines, balanced</span>{/if}
						</td>
						<td><button class="btn link" onclick={() => (shown = s.step)}>{shown === s.step ? 'Shown' : 'Show'}</button></td>
					</tr>
				{/each}
			</tbody>
		</table>

		{#if shownStep}
			<div class="lgrid">
				<section>
					<h4>Step {shownStep.step} journal <span class="muted">({shownStep.code})</span></h4>
					{#if shownStep.error}<div class="notice bad">{shownStep.error.code} — {shownStep.error.message}</div>{/if}
					{#if shownStep.plan?.lines.length}
						<table class="data">
							<thead><tr><th>Account</th><th>Entity</th><th>Movement</th><th class="num">Debit</th><th class="num">Credit</th></tr></thead>
							<tbody>
								{#each shownStep.plan.lines as l (l.lineNo)}
									<tr class:problem={!!l.problem}>
										<td><span class="mono"><b>{l.account ?? '—'}</b></span> <span class="small muted">{acctName(l.account)}</span></td>
										<td class="mono">{l.entity}</td>
										<td class="small">{l.movement ? movementWord(l.movement) : '—'}</td>
										<td class="num">{l.side === 'D' ? amount(l.amount) : ''}</td>
										<td class="num">{l.side === 'C' ? amount(l.amount) : ''}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					{/if}
				</section>
				<section>
					<h4>Temporary ledger after step {shownStep.step}</h4>
					{#if shownStep.balances.length}
						<table class="data">
							<thead><tr><th>Entity</th><th>Account</th><th>Subledger</th><th class="num">Debit</th><th class="num">Credit</th><th class="num" title="Debit minus credit">Balance (Dr − Cr)</th></tr></thead>
							<tbody>
								{#each shownStep.balances as b (`${b.entity} ${b.currency} ${b.account} ${b.value ?? ''}`)}
									<tr>
										<td class="mono">{b.entity} <span class="muted">{b.currency}</span></td>
										<td><span class="mono"><b>{b.account}</b></span> <span class="small muted">{acctName(b.account)}</span></td>
										<td class="mono small">{b.key ? `${b.key} ${b.value}` : '—'}</td>
										<td class="num">{amount(b.debit)}</td>
										<td class="num">{amount(b.credit)}</td>
										<td class="num"><b>{amount(b.balance)}</b></td>
									</tr>
								{/each}
							</tbody>
						</table>
					{:else}
						<p class="small muted">Nothing in the temporary ledger yet.</p>
					{/if}
				</section>
			</div>
		{/if}
	{/if}
</section>

<style>
	.grow {
		flex: 1;
	}
	.pick {
		margin: 6px 0 10px;
		align-items: end;
	}
	.pick .lbl {
		flex: 0 1 480px;
	}
	.steps {
		margin: 0 0 10px;
		padding-left: 22px;
		display: grid;
		gap: 4px;
	}
	.steps li > :global(*) {
		vertical-align: middle;
	}
	.steps li {
		display: list-item;
	}
	.sum {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		margin: 10px 0;
	}
	tr.on td {
		background: var(--gl-accent-soft);
	}
	tr.problem td {
		background: var(--gl-bad-soft);
	}
	.lgrid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
		gap: 14px;
		margin-top: 12px;
	}
	h4 {
		margin: 0 0 6px;
	}
	.life :global(table.data) {
		width: 100%;
	}
</style>
