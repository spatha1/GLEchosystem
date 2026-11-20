<script lang="ts">
	// One business event as a guided flow, in the order GL processes it: event details →
	// measures and formulas → dimensions, filters, dates and keys → accounting rules →
	// debit/credit lines and accounts → required source fields → mapping → collection →
	// simulation → journal → subledger → reconciliation → reports. Each step shows its state;
	// every configuration step is editable in a draft.
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { EventView } from '../lib/studio';
	import type { EventTab } from '../lib/route';
	import { glossary, ruleWhen, sideWord } from '../lib/explain';
	import { specs } from '../lib/sections';
	import Formula from './Formula.svelte';
	import LineageMap from './LineageMap.svelte';
	import FieldsTable from './FieldsTable.svelte';
	import Simulator from './Simulator.svelte';
	import RulePage from './RulePage.svelte';
	import SectionEditor from './SectionEditor.svelte';
	import StepNotBuilt from './StepNotBuilt.svelte';

	let { ctx }: { ctx: Ctx } = $props();

	let view = $state<EventView | null>(null);
	let failure = $state<ApiError | null>(null);
	let scenarioState = $state<'none' | 'pass' | 'fail' | 'unknown'>('unknown');
	let journal = $state(false);

	$effect(() => {
		const { api, version, route } = ctx;
		ctx.tick;
		api.event(version.configId, version.version, route.event!).then(
			(v) => {
				view = v;
				failure = null;
				const codes = v.scenarios.map((s) => s.code);
				if (!codes.length) scenarioState = 'none';
				else
					api.runScenarios(version.configId, version.version, codes).then(
						(r) => (scenarioState = r.results.every((x) => x.status === 'PASSED' || x.status === 'RAN') ? 'pass' : 'fail'),
						() => (scenarioState = 'unknown')
					);
			},
			(e) => (failure = e)
		);
	});

	type Status = 'ok' | 'warn' | 'bad' | 'off' | 'todo';
	interface Step {
		key: EventTab;
		n: number;
		label: string;
		status: Status;
		note: string;
	}

	const base = $derived({ configId: ctx.version.configId, version: ctx.version.version, event: ctx.route.event });
	const step = $derived<EventTab>(ctx.route.rule ? 'lines' : ctx.route.etab === 'overview' || !ctx.route.etab ? 'details' : ctx.route.etab);
	const known = $derived(view ? glossary(view) : {});
	const ev = $derived(ctx.route.event ?? '');
	const active = $derived(view?.rules.filter((r) => r.isActive) ?? []);
	const classes = $derived([...new Set(view?.rules.flatMap((r) => r.lines.map((l) => l.line.class)) ?? [])]);
	const errorsUnder = (prefixes: string[]) =>
		view?.findings.filter((f) => f.severity === 'error' && prefixes.some((p) => f.path === p || f.path.startsWith(p + '/'))).length ?? 0;

	const steps = $derived.by<Step[]>(() => {
		if (!view) return [];
		const v = view;
		const src = v.manifest.fields.filter((f) => f.kind === 'SOURCE').length;
		const ruleErr = errorsUnder(v.rules.map((r) => r.path));
		return [
			{ key: 'details', n: 4, label: 'Event details', status: errorsUnder([v.path]) ? 'bad' : v.authority.length ? 'ok' : 'warn', note: v.sourceSystems.map((s) => s.code).join(', ') || 'no source yet' },
			{ key: 'measures', n: 5, label: 'Measures and formulas', status: v.measures.length ? 'ok' : 'warn', note: `${v.measures.length} measures, ${v.derived.length} formulas` },
			{ key: 'context', n: 6, label: 'Dimensions, filters, dates, keys', status: 'ok', note: `${v.dimensions.length} dimensions, ${v.keys.length} keys` },
			{ key: 'rules', n: 7, label: 'Accounting rules', status: active.length ? (ruleErr ? 'bad' : 'ok') : 'bad', note: active.length ? `${active.length} active rule${active.length === 1 ? '' : 's'}` : 'no active rule' },
			{ key: 'lines', n: 8, label: 'Debit/credit lines and accounts', status: ruleErr ? 'bad' : active.length ? 'ok' : 'todo', note: `${active.reduce((n, r) => n + r.lines.length, 0)} lines, ${classes.length} classes` },
			{ key: 'fields', n: 9, label: 'Required source fields', status: src ? 'ok' : 'todo', note: `${src} fields to collect` },
			{ key: 'mapping', n: 10, label: 'Data Workbench mapping', status: 'off', note: 'not built yet' },
			{ key: 'collection', n: 11, label: 'Data collection', status: 'off', note: 'not built yet' },
			{
				key: 'simulate',
				n: 12,
				label: 'Rule execution (simulation)',
				status: scenarioState === 'pass' ? 'ok' : scenarioState === 'fail' ? 'bad' : 'todo',
				note: scenarioState === 'pass' ? `${v.scenarios.length} scenario(s) hold` : scenarioState === 'fail' ? 'a scenario fails' : scenarioState === 'none' ? 'no scenario yet' : 'run it'
			},
			{ key: 'journal', n: 13, label: 'GL journal', status: journal ? 'ok' : 'todo', note: journal ? 'balanced journal produced' : 'from the simulation' },
			{ key: 'subledger', n: 14, label: 'Subledger', status: 'off', note: 'not built yet' },
			{ key: 'reconciliation', n: 15, label: 'Reconciliation', status: 'off', note: 'not built yet' },
			{ key: 'reports', n: 16, label: 'Reports and files', status: 'off', note: 'not built yet' }
		];
	});
	const idx = $derived(steps.findIndex((s) => s.key === step));
	const cur = $derived(steps[idx]);
	const icon: Record<Status, string> = { ok: '✓', warn: '!', bad: '!', off: '–', todo: '•' };
	const to = (k: EventTab) => ctx.go({ ...base, etab: k });
	const firstRule = $derived(view?.rules[0]?.rule.code);
</script>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if !view}
	<p class="muted">Loading event {ev}…</p>
{:else}
	<section class="evhead">
		<div>
			<span class="kind">Business event</span>
			<h2 class="mono">{view.event.code}</h2>
			<p class="purpose">{view.event.description || 'No business purpose written yet.'}</p>
		</div>
		<div class="row">
			{#each view.sourceSystems as s (s.code)}<span class="chip">Source: <b>{s.code}</b></span>{/each}
			<button class="btn" class:primary={step === 'lineage'} onclick={() => to('lineage')}>Lineage map</button>
		</div>
	</section>

	<div class="flow">
		<nav class="stepper" aria-label="Steps for {view.event.code}">
			<ol>
				<li class="ctxstep"><button onclick={() => ctx.go({})}><span class="num ok">✓</span><span><b>Client</b><small>{ctx.me.tenantId}</small></span></button></li>
				<li class="ctxstep">
					<button onclick={() => ctx.go({ configId: ctx.version.configId, version: ctx.version.version })}
						><span class="num ok">✓</span><span><b>Configuration version</b><small>{ctx.version.configId} v{ctx.version.version} · {ctx.version.status}</small></span></button
					>
				</li>
				<li class="ctxstep">
					<button onclick={() => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, tab: 'events' })}
						><span class="num ok">✓</span><span><b>Business event</b><small>{view.event.code}</small></span></button
					>
				</li>
				{#each steps as s (s.key)}
					<li class:current={s.key === step} class={s.status}>
						<button onclick={() => to(s.key)} aria-current={s.key === step ? 'step' : undefined}>
							<span class="num {s.status}" title={s.status === 'off' ? 'Not built yet' : ''}>{s.key === step ? s.n : icon[s.status] === '•' ? s.n : icon[s.status]}</span>
							<span><b>{s.n}. {s.label}</b><small>{s.note}</small></span>
						</button>
					</li>
				{/each}
			</ol>
		</nav>

		<div class="content">
			{#if step === 'lineage'}
				<div class="stephead"><h3>Lineage map</h3><span class="small muted">every dependency of {view.event.code}, from source fields to accounts</span></div>
				<LineageMap graph={view.graph} {known} selected={ctx.route.select} onselect={(id) => ctx.go({ ...base, etab: 'lineage', select: id })} />
			{:else if cur}
				<div class="stephead">
					<h3><span class="sn">{cur.n}</span> {cur.label}</h3>
					{#if cur.status === 'off'}<span class="badge off">Not built yet</span>{/if}
				</div>

				{#if step === 'details'}
					<p class="lead">
						<b>{view.event.code}</b>
						{view.event.description ? `— ${view.event.description}.` : ''} It comes from {view.authority.length
							? view.authority.map((a) => `${a.sourceSystem} (${a.intakeMode === 'RAW_EVENT' ? 'raw events: GL builds the journal' : 'source journals'})`).join(', ')
							: 'no source yet'}, and is identified by its economic key <span class="mono">{view.event.economicKey.join(' + ')}</span>.
					</p>
					<SectionEditor
						{ctx}
						spec={specs.events}
						filter={(x) => x.code === ev}
						canAdd={false}
						title="Event"
						help="Purpose, source, economic key, measures, dimensions and lists of this event. Edit it to change any of them."
						findings={view.findings}
						onsaved={(code) => code && code !== ev && ctx.go({ ...base, event: code })}
					/>
					<SectionEditor {ctx} spec={specs.sourceAuthority} filter={(x) => x.eventType === ev} fixed={{ eventType: ev }} findings={view.findings} />
					<SectionEditor {ctx} spec={specs.sourceSystems} findings={view.findings} help="The insurance systems of this client (shared by all events)." />
				{:else if step === 'measures'}
					<section class="card">
						<h3>Measures {view.event.code} carries</h3>
						<table class="data">
							<thead><tr><th>Measure</th><th>Meaning</th><th>Grain</th><th>Used</th></tr></thead>
							<tbody>
								{#each view.measures as m (m.list + m.code)}
									<tr>
										<td class="mono"><b>{m.code}</b></td>
										<td class="small">{m.name}<div class="muted">{m.dataType.toLowerCase()} · {m.origin.toLowerCase()}</div></td>
										<td class="small">{m.list ? `each ${m.list} item` : 'event'}</td>
										<td>{#if m.required}<span class="badge ok">needed</span>{:else}<span class="badge off">not used</span>{/if}</td>
									</tr>
								{:else}<tr><td colspan="4" class="muted">None yet: edit the event in step 4 to choose its measures.</td></tr>{/each}
							</tbody>
						</table>
						<p class="small muted">To add or remove a measure, edit the event in step 4 (Measures). Measures come from the standard catalog or from custom measures below.</p>
					</section>
					<SectionEditor
						{ctx}
						spec={specs.derivedMeasures}
						filter={(x) => !x.eventTypes?.length || x.eventTypes.includes(ev)}
						fixed={{ eventTypes: [ev] }}
						title="Formulas for {ev}"
						findings={view.findings}
					/>
					<SectionEditor {ctx} spec={specs.baseMeasures} findings={view.findings} />
				{:else if step === 'context'}
					<section class="card">
						<h3>Filters</h3>
						<p class="small muted">A filter decides which {ev} events a posting rule applies to; a line condition decides whether a line is produced. They are edited with the rule (step 7–8).</p>
						<table class="data">
							<thead><tr><th>Rule</th><th>Applies when</th><th>Line conditions</th></tr></thead>
							<tbody>
								{#each view.rules as r (r.rule.code)}
									<tr>
										<td><button class="btn link mono" onclick={() => ctx.go({ ...base, rule: r.rule.code })}>{r.rule.code}</button></td>
										<td>{#if r.rule.filter}<Formula src={r.rule.filter} {known} />{:else}<span class="muted">every {ev}</span>{/if}</td>
										<td class="small">
											{#each r.lines.filter((l) => l.line.when) as l (l.line.code)}<div><span class="mono">{l.line.code}</span>: <Formula src={l.line.when ?? ''} {known} /></div>{:else}<span class="muted">none</span>{/each}
										</td>
									</tr>
								{:else}<tr><td colspan="3" class="muted">No rule yet.</td></tr>{/each}
							</tbody>
						</table>
					</section>
					<SectionEditor {ctx} spec={specs.dimensions} findings={view.findings} />
					<SectionEditor {ctx} spec={specs.lookups} findings={view.findings} />
					<SectionEditor {ctx} spec={specs.keyProfiles} findings={view.findings} />
					<SectionEditor {ctx} spec={specs.datePolicies} findings={view.findings} />
					<SectionEditor {ctx} spec={specs.calendars} findings={view.findings} />
				{:else if step === 'rules'}
					<div class="row bar">
						<p class="muted small grow">
							A posting rule turns one {ev} event into journal lines; the lowest-ranked matching rule applies and a tie is refused. Open a rule (step 8) to see and
							edit its debit and credit lines.
						</p>
						{#if ctx.canEdit}
							<button class="btn primary" onclick={() => ctx.go({ ...base, rule: '+new', edit: true })}>+ Add posting rule</button>
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
									<button class="btn" onclick={() => ctx.go({ ...base, rule: r.rule.code })}>Lines →</button>
								</div>
								{#if r.rule.description}<p class="desc">{r.rule.description}</p>{/if}
								<p class="small muted">{ruleWhen(r, ev)}</p>
								<div class="row">
									{#each r.lines as l (l.line.code)}<span class="chip"><span class="side-{l.line.side}">{sideWord(l.line.side)}</span>&nbsp;<span class="mono">{l.line.class}</span>&nbsp;= <span class="mono">{l.line.amount}</span></span>{/each}
								</div>
							</article>
						{:else}
							<div class="card"><p class="muted">No posting rule yet: {ev} events are refused (MISSING_RULE).</p></div>
						{/each}
					</div>
				{:else if step === 'lines'}
					{#if view.rules.length > 1 || ctx.route.rule === '+new'}
						<div class="row pick">
							<span class="small muted">Rule:</span>
							{#each view.rules as r (r.rule.code)}
								<button class="btn" class:primary={(ctx.route.rule ?? firstRule) === r.rule.code} onclick={() => ctx.go({ ...base, rule: r.rule.code })}>{r.rule.code}</button>
							{/each}
						</div>
					{/if}
					{#if ctx.route.rule || firstRule}
						{#key ctx.route.rule ?? firstRule}
							<RulePage ctx={ctx.route.rule ? ctx : { ...ctx, route: { ...ctx.route, rule: firstRule } }} {view} />
						{/key}
					{:else}
						<div class="card"><p class="muted">No posting rule yet. Add one in step 7.</p></div>
					{/if}
					<h3 class="sub">Accounts for these lines</h3>
					<SectionEditor {ctx} spec={specs.accountingClasses} filter={(x) => classes.includes(x.code)} title="Accounting classes used by {ev}" findings={view.findings} />
					<SectionEditor {ctx} spec={specs.accounts} filter={(x) => classes.includes(x.class)} title="Accounts of those classes" findings={view.findings} />
					<SectionEditor {ctx} spec={specs.accountAssignmentRules} filter={(x) => classes.includes(x.class)} title="How each line's account is chosen" findings={view.findings} />
				{:else if step === 'fields'}
					<FieldsTable explanation={view.explanation} manifest={view.manifest} {known} graph={view.graph} />
				{:else if step === 'simulate' || step === 'journal'}
					<Simulator {ctx} fixedEvent={view} section={step === 'journal' ? 'journal' : 'run'} onjournal={(r) => (journal = r)} />
				{:else if step === 'mapping' || step === 'collection' || step === 'subledger' || step === 'reconciliation' || step === 'reports'}
					<StepNotBuilt {ctx} {view} {step} />
				{/if}

				<div class="row nav">
					{#if idx > 0}<button class="btn" onclick={() => to(steps[idx - 1].key)}>← {steps[idx - 1].n}. {steps[idx - 1].label}</button>{/if}
					<span class="spacer"></span>
					{#if idx < steps.length - 1}<button class="btn primary" onclick={() => to(steps[idx + 1].key)}>{steps[idx + 1].n}. {steps[idx + 1].label} →</button>{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}

<style>
	.evhead {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: flex-end;
		gap: 12px;
		margin: 18px 0 14px;
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
	.flow {
		display: grid;
		grid-template-columns: 270px minmax(0, 1fr);
		gap: 18px;
		align-items: start;
	}
	@media (max-width: 960px) {
		.flow {
			grid-template-columns: 1fr;
		}
	}
	.stepper {
		position: sticky;
		top: 10px;
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 8px;
	}
	.stepper ol {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 1px;
	}
	.stepper button {
		font: inherit;
		width: 100%;
		display: flex;
		gap: 10px;
		align-items: center;
		text-align: left;
		padding: 6px 8px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--gl-text);
		cursor: pointer;
	}
	.stepper button:hover {
		background: var(--gl-surface-2);
	}
	.stepper li.current button {
		background: var(--gl-accent-soft);
	}
	.stepper b {
		display: block;
		font-size: 12.5px;
		font-weight: 600;
	}
	.stepper small {
		display: block;
		font-size: 11px;
		color: var(--gl-muted);
	}
	.stepper li.off b {
		color: var(--gl-muted);
		font-weight: 500;
	}
	.ctxstep {
		opacity: 0.85;
	}
	.ctxstep:nth-child(3) {
		border-bottom: 1px solid var(--gl-border);
		padding-bottom: 4px;
		margin-bottom: 4px;
	}
	.num {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		font-size: 11px;
		font-weight: 700;
		border: 1px solid var(--gl-border-strong);
		color: var(--gl-muted);
		background: var(--gl-surface);
	}
	.num.ok {
		background: var(--gl-ok);
		border-color: var(--gl-ok);
		color: #fff;
	}
	.num.bad {
		background: var(--gl-bad);
		border-color: var(--gl-bad);
		color: #fff;
	}
	.num.warn {
		background: var(--gl-warn-soft);
		border-color: var(--gl-warn);
		color: var(--gl-warn);
	}
	.num.off {
		border-style: dashed;
		background: var(--gl-off-soft);
	}
	li.current .num {
		background: var(--gl-accent);
		border-color: var(--gl-accent);
		color: var(--gl-accent-ink);
	}
	.content {
		min-width: 0;
		display: grid;
		gap: 14px;
		align-content: start;
	}
	.content :global(.sec + .sec) {
		margin-top: 0;
	}
	.stephead {
		display: flex;
		gap: 10px;
		align-items: center;
	}
	.stephead h3 {
		margin: 0;
		font-size: 17px;
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.sn {
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		font-size: 13px;
	}
	.lead {
		margin: 0;
		font-size: 14.5px;
	}
	.bar {
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
	.pick {
		gap: 6px;
	}
	.sub {
		margin: 8px 0 0;
		font-size: 15px;
	}
	.nav {
		margin-top: 6px;
		padding-top: 12px;
		border-top: 1px solid var(--gl-border);
	}
</style>
