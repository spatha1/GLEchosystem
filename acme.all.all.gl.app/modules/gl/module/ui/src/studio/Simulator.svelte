<script lang="ts">
	// Simulation: one event through one configuration version, as a posting preview (nothing is
	// stored or posted). The input form is generated from the event's configuration, and the
	// sample events are the version's own scenarios — nothing here is hard-coded.
	import { untrack } from 'svelte';
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { Account, EventInput, EventSummary, EventView, Plan, Scenario, ScenarioResult } from '../lib/studio';
	import type { PlannedLine } from '../lib/types';
	import { clone, uniqueCode, withScenario } from '../lib/content';
	import { amount } from '../lib/format';
	import { glossary, movementWord, natureWord, sideWord } from '../lib/explain';
	import Formula from './Formula.svelte';
	import LegLineage from './LegLineage.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let {
		ctx,
		fixedEvent,
		section = 'all',
		onjournal
	}: { ctx: Ctx; fixedEvent?: EventView; section?: 'all' | 'run' | 'journal'; onjournal?: (ready: boolean) => void } = $props();
	const showRun = $derived(section !== 'journal');
	const showJournal = $derived(section !== 'run');

	let events = $state<EventSummary[]>([]);
	let eventCode = $state(untrack(() => fixedEvent?.event.code ?? ''));
	let view = $state<EventView | null>(untrack(() => fixedEvent ?? null));
	let scenarioCode = $state('');
	let input = $state<EventInput>({ currency: '' });
	let inputFor = $state(''); // the event the form's input was built for
	let pristine = $state(true);
	let plan = $state<Plan | null>(null);
	let refusal = $state<{ code: string; message: string } | null>(null);
	let check = $state<ScenarioResult | null>(null);
	let running = $state(false);
	let selected = $state<number | null>(null);
	let all = $state<ScenarioResult[] | null>(null);
	let saving = $state(false);
	let asking = $state(false);
	let saveError = $state('');

	const known = $derived(view ? glossary(view) : {});
	const accounts = $derived((ctx.content.accounts ?? []) as Account[]);
	const acctName = (code?: string) => accounts.find((a) => a.code === code)?.name ?? '';
	const scenarios = $derived(view?.scenarios ?? []);
	const eventKeys = $derived(view?.keys.filter((k) => k.roles.some((r) => r === 'ECONOMIC_KEY' || r === 'EVENT_KEY')) ?? []);
	const carried = $derived(view?.dimensions.filter((d) => d.carried) ?? []);
	const eventMeasures = $derived(view?.measures.filter((m) => !m.list) ?? []);
	const lists = $derived(view?.event.lists ?? []);

	// Version-level: choose the event; event-level: it is fixed.
	$effect(() => {
		if (fixedEvent) return;
		ctx.tick;
		ctx.api.events(ctx.version.configId, ctx.version.version).then((r) => {
			events = r.events;
			const wanted = ctx.route.scenario ? (ctx.content.scenarios ?? []).find((s: Scenario) => s.code === ctx.route.scenario)?.eventType : undefined;
			if (!eventCode) eventCode = wanted ?? r.events[0]?.code ?? '';
		});
	});
	$effect(() => {
		if (fixedEvent || !eventCode) return;
		const code = eventCode;
		ctx.api.event(ctx.version.configId, ctx.version.version, code).then((v) => {
			if (code === eventCode) view = v;
		});
	});
	// Pick the scenario named in the route, else the event's first.
	$effect(() => {
		if (!view) return;
		const fromRoute = view.scenarios.find((s) => s.code === ctx.route.scenario);
		const s = fromRoute ?? view.scenarios[0];
		untrack(() => load(s?.code ?? ''));
	});

	function blank(v: EventView): EventInput {
		const keys: Record<string, string> = {};
		for (const k of v.keys.filter((k) => k.roles.some((r) => r === 'ECONOMIC_KEY' || r === 'EVENT_KEY'))) keys[k.code] = '';
		const dims: Record<string, string> = {};
		for (const d of v.dimensions.filter((d) => d.carried)) dims[d.code] = '';
		const ms: Record<string, string> = {};
		for (const m of v.measures.filter((m) => !m.list)) ms[m.code] = '';
		const ls: Record<string, Record<string, string>[]> = {};
		for (const l of v.event.lists ?? []) ls[l.code] = [];
		return { sourceSystem: v.sourceSystems[0]?.code, sourceEventId: '', sourceEventVersion: 1, currency: '', transactionDate: '', effectiveDate: '', keys, dimensions: dims, measures: ms, lists: ls };
	}

	function load(code: string) {
		if (!view) return;
		scenarioCode = code;
		const s = view.scenarios.find((x) => x.code === code);
		const b = blank(view);
		if (s) {
			const i = clone(s.input);
			input = { ...b, ...i, keys: { ...b.keys, ...i.keys }, dimensions: { ...b.dimensions, ...i.dimensions }, measures: { ...b.measures, ...i.measures }, lists: { ...b.lists, ...i.lists } };
		} else {
			input = b;
		}
		inputFor = view.event.code;
		pristine = true;
		plan = null;
		refusal = null;
		check = null;
		selected = null;
		if (s) run();
	}

	function body(): EventInput {
		const strip = (o?: Record<string, string>) => Object.fromEntries(Object.entries(o ?? {}).filter(([, v]) => `${v}`.trim() !== ''));
		const out: EventInput = { ...clone(input), eventType: view!.event.code };
		out.keys = strip(out.keys);
		out.dimensions = strip(out.dimensions);
		out.measures = strip(out.measures);
		for (const k of ['transactionDate', 'effectiveDate', 'sourceEventId'] as const) if (!out[k]) delete out[k];
		out.lists = Object.fromEntries(Object.entries(out.lists ?? {}).map(([k, rows]) => [k, rows.map((r) => strip(r))]));
		return out;
	}

	async function run() {
		if (!view) return;
		running = true;
		plan = null;
		refusal = null;
		check = null;
		selected = null;
		try {
			plan = await ctx.api.plan(ctx.version.configId, ctx.version.version, body());
		} catch (e) {
			const err = e as ApiError;
			refusal = { code: err.code, message: err.message };
			if (err.detail && typeof err.detail === 'object' && 'lines' in (err.detail as object)) plan = err.detail as Plan;
		}
		if (pristine && scenarioCode) {
			try {
				const r = await ctx.api.runScenarios(ctx.version.configId, ctx.version.version, [scenarioCode]);
				check = r.results[0] ?? null;
			} catch {
				check = null;
			}
		}
		const first = plan?.lines.find((l) => l.problem) ?? plan?.lines[0];
		selected = first?.lineNo ?? null;
		running = false;
		onjournal?.(!!plan && !refusal);
	}

	async function runAll() {
		const r = await ctx.api.runScenarios(ctx.version.configId, ctx.version.version);
		all = r.results;
	}

	function edited() {
		pristine = false;
	}

	// Journal entries: one per legal entity and currency, as they will post.
	const entries = $derived.by(() => {
		if (!plan) return [];
		const m = new Map<string, PlannedLine[]>();
		for (const l of plan.lines) m.set(`${l.entity} ${l.currency}`, [...(m.get(`${l.entity} ${l.currency}`) ?? []), l]);
		return [...m.entries()].map(([k, lines]) => ({ key: k, entity: lines[0].entity, currency: lines[0].currency, lines, total: plan!.totals.find((t) => t.entity === lines[0].entity && t.currency === lines[0].currency) }));
	});
	const sel = $derived(plan?.lines.find((l) => l.lineNo === selected));
	const postable = $derived(!!plan && plan.balanced && !refusal && plan.lines.every((l) => !l.problem));

	async function saveScenario(reason: string) {
		if (!plan || !view) return;
		saving = true;
		saveError = '';
		// Started from a scenario: update it. Started blank: a new code, never over an existing scenario.
		const taken = ((ctx.content.scenarios ?? []) as Scenario[]).map((s) => s.code);
		const code = scenarioCode ? scenarioCode.toUpperCase() : uniqueCode(`${view.event.code}_SIM`, taken);
		const s: Scenario = {
			code,
			eventType: view.event.code,
			description: view.scenarios.find((x) => x.code === scenarioCode)?.description,
			input: (({ eventType, ...rest }) => rest)(body()),
			expect: refusal ? { refused: refusal.code } : { rule: plan.rule, legs: plan.lines.map((l) => ({ class: l.class, side: l.side, amount: l.amount, entity: l.entity, account: l.account })) }
		};
		try {
			await ctx.save(withScenario(ctx.content, s), reason);
			asking = false;
		} catch (e) {
			const err = e as ApiError;
			saveError = `${err.code}: ${err.message}`;
			asking = false;
		} finally {
			saving = false;
		}
	}
</script>

<div class="sim">
	{#if !showRun}
		<section class="card">
			{#if plan}
				<p class="small">
					The journal below is what <b>{ctx.version.configId} v{ctx.version.version}</b> produces for the {scenarioCode ? `scenario ${scenarioCode}` : 'simulated'}
					{view?.event.code} event of step 12. Nothing is posted: posting is not built yet (US-019 to US-021). Select a line to see where it comes from.
				</p>
			{:else}
				<p class="small">No journal yet: run a simulation in step 12 first.</p>
			{/if}
		</section>
	{/if}
	<section class="card inputs" class:hidden={!showRun}>
		<div class="row">
			<h3 class="grow">Simulate {view ? view.event.code : 'an event'} <span class="muted">· configuration {ctx.version.configId} v{ctx.version.version} ({ctx.version.status})</span></h3>
			<button class="btn" onclick={runAll}>Run all scenarios</button>
		</div>
		<p class="small muted">A posting preview: the engine runs this version's rules exactly as it will post, but nothing is stored or posted.</p>
		<div class="row pick">
			{#if !fixedEvent}
				<label class="lbl">Event
					<select class="field" bind:value={eventCode}>
						{#each events as e (e.code)}<option value={e.code}>{e.code}</option>{/each}
					</select>
				</label>
			{/if}
			<label class="lbl">Start from
				<select class="field" value={scenarioCode} onchange={(e) => load((e.currentTarget as HTMLSelectElement).value)}>
					{#each scenarios as s (s.code)}<option value={s.code}>Scenario {s.code}{s.description ? ` — ${s.description}` : ''}</option>{/each}
					<option value="">Blank {view?.event.code ?? ''} event</option>
				</select>
			</label>
		</div>

		{#if view && inputFor === view.event.code}
			<div class="form" role="group" aria-label="Event input" oninput={edited}>
				<fieldset>
					<legend>Source record</legend>
					<label class="lbl">Source system
						<select class="field" bind:value={input.sourceSystem}>
							{#each view.sourceSystems as s (s.code)}<option value={s.code}>{s.code}</option>{/each}
						</select>
					</label>
					<label class="lbl">Record id <input class="field mono" bind:value={input.sourceEventId} /></label>
					<label class="lbl">Version <input class="field" type="number" min="1" bind:value={input.sourceEventVersion} /></label>
				</fieldset>
				<fieldset>
					<legend>Dates and currency</legend>
					<label class="lbl">Transaction date <input class="field" type="date" bind:value={input.transactionDate} /></label>
					<label class="lbl">Effective date <input class="field" type="date" bind:value={input.effectiveDate} /></label>
					<label class="lbl">Currency <input class="field mono" maxlength="3" bind:value={input.currency} placeholder="ISO code" /></label>
				</fieldset>
				{#if eventKeys.length}
					<fieldset>
						<legend>Business keys</legend>
						{#each eventKeys as k (k.code)}
							<label class="lbl">{k.code} <input class="field mono" bind:value={input.keys![k.code]} placeholder={k.pattern ?? ''} /></label>
						{/each}
					</fieldset>
				{/if}
				{#if carried.length}
					<fieldset>
						<legend>Dimensions</legend>
						{#each carried as d (d.code)}
							<label class="lbl"
								>{d.code}
								{#if d.values?.length}
									<select class="field" bind:value={input.dimensions![d.code]}>
										<option value="">—</option>
										{#each d.values as v (v)}<option value={v}>{v}</option>{/each}
									</select>
								{:else}
									<input class="field mono" bind:value={input.dimensions![d.code]} />
								{/if}
							</label>
						{/each}
					</fieldset>
				{/if}
				{#if eventMeasures.length}
					<fieldset>
						<legend>Measures</legend>
						{#each eventMeasures as m (m.code)}
							<label class="lbl">{m.code} <input class="field mono amt" inputmode="decimal" bind:value={input.measures![m.code]} placeholder="0.00" /></label>
						{/each}
					</fieldset>
				{/if}
				{#each lists as l (l.code)}
					{@const cols = [...l.itemKey, ...(l.keys ?? []).filter((k) => !l.itemKey.includes(k)), ...(l.measures ?? [])]}
					<fieldset class="list">
						<legend>{l.code} <span class="muted">(one row per item)</span></legend>
						<table class="data">
							<thead><tr>{#each cols as c (c)}<th>{c}</th>{/each}<th></th></tr></thead>
							<tbody>
								{#each input.lists![l.code] ?? [] as row, i (i)}
									<tr>
										{#each cols as c (c)}<td><input class="field mono" bind:value={row[c]} aria-label="{l.code} item {i + 1} {c}" /></td>{/each}
										<td><button class="btn link" onclick={() => { input.lists![l.code] = input.lists![l.code].filter((_, j) => j !== i); edited(); }}>Remove</button></td>
									</tr>
								{/each}
							</tbody>
						</table>
						<button class="btn" onclick={() => { input.lists![l.code] = [...(input.lists![l.code] ?? []), Object.fromEntries(cols.map((c) => [c, '']))]; edited(); }}>Add item</button>
					</fieldset>
				{/each}
			</div>
			<div class="row runrow">
				<button class="btn primary" onclick={run} disabled={running}>{running ? 'Running…' : 'Run simulation'}</button>
				{#if !pristine && scenarioCode}<span class="small muted">Inputs changed from scenario {scenarioCode}: its expectation is not checked.</span>{/if}
				<span class="spacer"></span>
				{#if ctx.canEdit && plan}
					<button class="btn" onclick={() => (asking = true)}>Save as scenario in draft</button>
				{/if}
			</div>
			{#if saveError}<div class="notice bad">{saveError}</div>{/if}
		{/if}
	</section>

	{#if all}
		<section class="card">
			<div class="row"><h3 class="grow">All scenarios of v{ctx.version.version}</h3><button class="btn link" onclick={() => (all = null)}>Close</button></div>
			<table class="data">
				<thead><tr><th>Scenario</th><th>Event</th><th>Result</th><th>Details</th></tr></thead>
				<tbody>
					{#each all as r (r.code)}
						<tr>
							<td class="mono"><b>{r.code}</b><div class="small muted">{r.description ?? ''}</div></td>
							<td class="mono">{r.eventType}</td>
							<td>
								<span class="badge {r.status === 'PASSED' ? 'ok' : r.status === 'RAN' ? 'info' : 'bad'}">{r.status === 'PASSED' ? 'holds' : r.status.toLowerCase()}</span>
							</td>
							<td class="small">
								{#if r.mismatches.length}{r.mismatches.join('; ')}{:else if r.error}{r.error.code}: {r.error.message}{:else}{r.plan?.lines.length} legs, balanced{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</section>
	{/if}

	{#if refusal}
		<div class="notice bad" role="alert"><strong>Refused: {refusal.code}</strong> — {refusal.message}</div>
	{/if}

	{#if plan && view}
		<section class="summary" class:hidden={!showRun && !showJournal}>
			<div><span class="k">Configuration</span><span class="v mono">{plan.config.configId} v{plan.config.configVersion}</span><span class="s">{ctx.version.status}</span></div>
			<div><span class="k">Event</span><span class="v mono">{plan.eventType}</span><span class="s">{input.sourceSystem} {input.sourceEventId}</span></div>
			<div>
				<span class="k">Matched rule</span><button class="btn link v mono" onclick={() => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event: plan!.eventType, rule: plan!.rule })}>{plan.rule || '—'}</button><span class="s">rank {plan.rank}</span>
			</div>
			<div><span class="k">Book date</span><span class="v">{plan.bookDate ?? '—'}</span><span class="s">{plan.period ? `period ${plan.period.accountingPeriod}${plan.period.bookingClass ? `, class ${plan.period.bookingClass}` : ''}` : 'no period decided'}</span></div>
			<div><span class="k">Balance</span><span class="badge {plan.balanced ? 'ok' : 'bad'}">{plan.balanced ? 'Balanced' : 'Not balanced'}</span></div>
			<div><span class="k">Posting</span><span class="badge {postable ? 'ok' : 'bad'}">{postable ? 'Can post' : 'Refused'}</span></div>
			{#if check}
				<div>
					<span class="k">Scenario {check.code}</span>
					<span class="badge {check.status === 'PASSED' ? 'ok' : check.status === 'RAN' ? 'info' : 'bad'}">{check.status === 'PASSED' ? 'Matches expectation' : check.status === 'RAN' ? 'No expectation' : 'Differs from expectation'}</span>
				</div>
			{/if}
		</section>
		{#if check?.mismatches.length}
			<div class="notice warn">{check.mismatches.join(' · ')}</div>
		{/if}

		<div class="grid2" class:hidden={!showRun}>
			<section class="card">
				<h3>Why this rule</h3>
				<ul class="plain">
					{#each plan.considered as c (c.rule)}
						<li class:matched={c.matched && c.rule === plan.rule}><span class="mono"><b>{c.rule}</b></span> <span class="muted">rank {c.rank}</span> — {c.reason}</li>
					{/each}
				</ul>
				{#if plan.skipped.length}
					<h4>Lines not produced</h4>
					<ul class="plain">{#each plan.skipped as s, i (i)}<li class="muted">{s}</li>{/each}</ul>
				{/if}
			</section>
			<section class="card">
				<h3>Calculations</h3>
				{#each plan.derived as d (d.code)}
					<div class="calc">
						<span class="mono"><b>{d.code}</b></span> = <Formula src={d.formula} {known} />
						<div class="small">= {(d.trace?.reads ?? []).map((r) => `${r.name} ${r.value}`).join(', ')} → <b class="mono">{d.value}</b></div>
					</div>
				{/each}
				{#each plan.dimensions.filter((x) => x.via !== 'EVENT') as x (x.dimension)}
					<div class="calc">
						<span class="mono"><b>{x.dimension}</b></span> = <b class="mono">{x.value}</b>
						<div class="small muted">{x.via === 'LOOKUP' ? `lookup ${x.lookup} on ${x.key}` : `date policy ${x.lookup}`}</div>
					</div>
				{/each}
				{#if !plan.derived.length && !plan.dimensions.some((x) => x.via !== 'EVENT')}<p class="muted small">Nothing calculated for this event.</p>{/if}
			</section>
		</div>

		{#if section === 'run'}
			<div class="notice ok row">
				<span class="grow">The rule produced {plan.lines.length} journal line{plan.lines.length === 1 ? '' : 's'} ({plan.balanced ? 'balanced' : 'not balanced'}).</span>
				<button class="btn primary" onclick={() => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event: view!.event.code, etab: 'journal' })}>See the GL journal (step 13) →</button>
			</div>
		{/if}
		<section class="card journal" class:hidden={!showJournal}>
			<h3>Journal <span class="muted">— one entry per legal entity and currency; select a line to see where it comes from</span></h3>
			{#each entries as e (e.key)}
				<div class="entry">
					<div class="row ehead">
						<span class="mono"><b>{e.entity}</b></span><span class="muted">{e.currency}</span>
						<span class="spacer"></span>
						{#if e.total}<span class="badge {e.total.balanced ? 'ok' : 'bad'}">{e.total.balanced ? 'balanced' : 'not balanced'} · Dr {amount(e.total.debit)} · Cr {amount(e.total.credit)}</span>{/if}
					</div>
					<table class="data jt">
						<colgroup><col style="width:4%" /><col style="width:22%" /><col style="width:8%" /><col style="width:15%" /><col style="width:11%" /><col style="width:11%" /><col style="width:11%" /><col /></colgroup>
						<thead><tr><th>#</th><th>Account</th><th>Class</th><th>Movement</th><th class="num">Debit</th><th class="num">Credit</th><th>Rule line</th><th>Keys</th></tr></thead>
						<tbody>
							{#each e.lines as l (l.lineNo)}
								<tr class="leg" class:on={selected === l.lineNo} class:problem={!!l.problem} onclick={() => (selected = l.lineNo)}>
									<td>{l.lineNo}</td>
									<td><span class="mono"><b>{l.account ?? '—'}</b></span> <span class="small muted">{acctName(l.account)}</span></td>
									<td class="mono">{l.class}</td>
									<td
										>{#if l.movement}<span class="mv {l.movement === 'INCREASE' ? 'inc' : 'dec'}">{movementWord(l.movement)}</span>
											<span class="small muted">{natureWord(l.nature)}, normally {sideWord(l.normalSide ?? 'D').toLowerCase()}</span>{:else}—{/if}</td
									>
									<td class="num side-D">{l.side === 'D' ? amount(l.amount) : ''}</td>
									<td class="num side-C">{l.side === 'C' ? amount(l.amount) : ''}</td>
									<td class="mono small">{l.template}{l.item ? ` #${l.item}` : ''}</td>
									<td class="small">{Object.entries(l.keys).map(([k, v]) => `${k} ${v}`).join(' · ')}</td>
								</tr>
								{#if l.problem}<tr class="prob"><td></td><td colspan="7"><div role="alert">{l.problem}</div></td></tr>{/if}
							{/each}
						</tbody>
					</table>
				</div>
			{/each}
		</section>

		{#if sel && showJournal}
			<LegLineage {ctx} {view} {plan} line={sel} {input} scenario={pristine ? scenarioCode : undefined} {known} />
		{/if}
	{/if}
</div>

{#if asking && view}
	<ReasonDialog
		title="Save scenario {(scenarioCode || `${view.event.code}_SIM`).toUpperCase()} to draft v{ctx.version.version}"
		verb="Save"
		detail="The inputs are saved with this result as the expected journal, so later rule changes are checked against it."
		busy={saving}
		onsubmit={saveScenario}
		oncancel={() => (asking = false)}
	/>
{/if}

<style>
	.mv {
		font-weight: 600;
		white-space: nowrap;
	}
	.mv.inc {
		color: var(--gl-ok);
	}
	.mv.dec {
		color: var(--gl-warn);
	}
	.sim {
		display: grid;
		gap: 14px;
	}
	.grow {
		flex: 1;
	}
	.hidden {
		display: none;
	}
	.pick {
		margin: 6px 0 10px;
	}
	.form {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 10px;
	}
	fieldset {
		border: 1px solid var(--gl-border);
		border-radius: 8px;
		padding: 8px 10px 10px;
		display: grid;
		gap: 6px;
		align-content: start;
		margin: 0;
		min-width: 0;
	}
	fieldset.list {
		grid-column: 1 / -1;
	}
	legend {
		font-size: 12px;
		font-weight: 650;
		padding: 0 4px;
	}
	.amt {
		text-align: right;
	}
	.runrow {
		margin-top: 12px;
	}
	.summary {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 10px;
	}
	.summary > div {
		display: grid;
		gap: 2px;
		padding: 10px 12px;
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
	}
	.summary .k {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
	}
	.summary .v {
		font-weight: 650;
		justify-self: start;
	}
	.summary .s {
		font-size: 11.5px;
		color: var(--gl-muted);
	}
	.summary .badge {
		justify-self: start;
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 4px;
		font-size: 13px;
	}
	.plain li {
		color: var(--gl-muted);
	}
	.plain li.matched {
		color: var(--gl-text);
	}
	.calc {
		padding: 6px 0;
		border-top: 1px solid var(--gl-border);
	}
	.calc:first-of-type {
		border-top: 0;
	}
	.entry + .entry {
		margin-top: 14px;
	}
	.ehead {
		padding: 6px 8px;
		background: var(--gl-surface-2);
		border-radius: 6px;
	}
	.jt {
		table-layout: fixed;
	}
	.leg {
		cursor: pointer;
	}
	.leg:hover td {
		background: var(--gl-surface-2);
	}
	.leg.on td {
		background: var(--gl-accent-soft);
	}
	.leg.problem td {
		background: var(--gl-bad-soft);
	}
	.prob td {
		color: var(--gl-bad);
		font-size: 12.5px;
	}
</style>
