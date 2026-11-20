<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { DerivedMeasure, EventView } from '../lib/studio';
	import type { Finding } from '../lib/types';
	import type { EventTab } from '../lib/route';
	import { under, uniqueCode, withDerived } from '../lib/content';
	import Formula from './Formula.svelte';
	import Findings from '../components/Findings.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let { ctx, view, known }: { ctx: Ctx; view: EventView; known: Record<string, string> } = $props();

	const base = $derived({ configId: ctx.version.configId, version: ctx.version.version, event: view.event.code });
	const to = (etab: EventTab) => ctx.go({ ...base, etab });

	const classes = $derived([...new Set(view.rules.filter((r) => r.isActive).flatMap((r) => r.lines.map((l) => l.line.class)))]);
	const accounts = $derived([
		...new Set(
			view.rules
				.filter((r) => r.isActive)
				.flatMap((r) => r.lines.flatMap((l) => l.candidates.map((c) => c.rule.account)))
		)
	]);
	const activeRules = $derived(view.rules.filter((r) => r.isActive));
	const derivedDims = $derived(view.dimensions.filter((d) => !d.carried));
	const usedBy = (id: string) =>
		view.graph.edges
			.filter((e) => e.from === id)
			.map((e) => view.graph.nodes.find((n) => n.id === e.to))
			.filter((n) => !!n)
			.map((n) => (n!.kind === 'LINE' ? `${n!.rule} · ${n!.code}` : n!.code));

	// ── formula editing (drafts only) ──
	let editing = $state<{ original?: string; dm: DerivedMeasure } | null>(null);
	let check = $state<Finding[]>([]);
	let asking = $state(false);
	let saving = $state(false);
	let saveError = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	function edit(code?: string) {
		const src = code ? ((ctx.content.derivedMeasures ?? []) as DerivedMeasure[]).find((d) => d.code === code) : undefined;
		editing = src
			? { original: code, dm: JSON.parse(JSON.stringify(src)) }
			: {
					dm: {
						code: uniqueCode(`${view.event.code}_VALUE`, ((ctx.content.derivedMeasures ?? []) as DerivedMeasure[]).map((d) => d.code)),
						formula: '',
						resultType: 'AMOUNT',
						eventTypes: [view.event.code]
					}
				};
		check = [];
		saveError = '';
	}

	$effect(() => {
		if (!editing) return;
		const candidate = withDerived(ctx.content, editing.dm, editing.original);
		const list = (candidate.derivedMeasures ?? []) as DerivedMeasure[];
		const idx = list.findIndex((d) => d.code === editing!.dm.code.trim());
		clearTimeout(timer);
		timer = setTimeout(() => {
			ctx.api.check(ctx.version.configId, ctx.version.version, candidate).then(
				(r) => (check = under(r.findings, `derivedMeasures/${idx}`)),
				() => (check = [])
			);
		}, 350);
	});

	async function save(reason: string) {
		if (!editing) return;
		saving = true;
		saveError = '';
		try {
			await ctx.save(withDerived(ctx.content, editing.dm, editing.original), reason);
			editing = null;
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

<section class="card chain">
	<h3>How {view.event.code} is processed <span class="muted">— click a step to see it</span></h3>
	<ol class="steps">
		<li>
			<button onclick={() => to('overview')}>
				<span class="n">1</span><span class="t">Source</span>
				<span class="v">{view.sourceSystems.map((s) => s.code).join(', ') || '—'}</span>
				<span class="s">{[...new Set(view.authority.map((a) => a.intakeMode.replace('_', ' ').toLowerCase()))].join(', ')}</span>
			</button>
		</li>
		<li>
			<button onclick={() => to('fields')}>
				<span class="n">2</span><span class="t">Fields collected</span>
				<span class="v">{view.manifest.fields.filter((f) => f.kind === 'SOURCE').length}</span>
				<span class="s">derived from the rules</span>
			</button>
		</li>
		<li>
			<button onclick={() => to('overview')}>
				<span class="n">3</span><span class="t">Measures</span>
				<span class="v mono">{view.measures.map((m) => m.code).join(', ') || '—'}</span>
				<span class="s">{view.derived.length} formula{view.derived.length === 1 ? '' : 's'}, {derivedDims.length} lookup{derivedDims.length === 1 ? '' : 's'}</span>
			</button>
		</li>
		<li>
			<button onclick={() => to('rules')}>
				<span class="n">4</span><span class="t">Posting rules</span>
				<span class="v mono">{activeRules.map((r) => r.rule.code).join(', ') || 'none'}</span>
				<span class="s">{activeRules.reduce((n, r) => n + r.lines.length, 0)} line templates</span>
			</button>
		</li>
		<li>
			<button onclick={() => to('lineage')}>
				<span class="n">5</span><span class="t">Classes → accounts</span>
				<span class="v mono">{classes.join(', ') || '—'}</span>
				<span class="s">{accounts.length} candidate account{accounts.length === 1 ? '' : 's'}</span>
			</button>
		</li>
		<li>
			<button onclick={() => to('simulate')}>
				<span class="n">6</span><span class="t">Journal</span>
				<span class="v">{view.scenarios.length} scenario{view.scenarios.length === 1 ? '' : 's'}</span>
				<span class="s">simulate to see the legs</span>
			</button>
		</li>
	</ol>
</section>

<div class="grid2 sections">
	<section class="card">
		<h3>Source and authority</h3>
		<p class="small muted">Which source system may post which measure of this event, and how (US-005).</p>
		<table class="data">
			<thead><tr><th>Measure</th><th>Source</th><th>Intake</th><th>Effective</th></tr></thead>
			<tbody>
				{#each view.authority as a, i (i)}
					<tr>
						<td class="mono">{a.measure === '*' ? 'all measures' : a.measure}</td>
						<td><b>{a.sourceSystem}</b></td>
						<td class="small">{a.intakeMode === 'RAW_EVENT' ? 'raw event (GL builds the journal)' : 'source journal (imported as balanced)'}</td>
						<td class="small">{a.effectiveFrom ?? 'always'}{a.effectiveTo ? ` → ${a.effectiveTo}` : ''}</td>
					</tr>
				{:else}
					<tr><td colspan="4" class="muted">No source is authoritative: such events cannot post.</td></tr>
				{/each}
			</tbody>
		</table>
	</section>

	<section class="card">
		<h3>Business keys</h3>
		<p class="small muted">How the event is identified and linked to policies, invoices and receipts (US-010).</p>
		<table class="data">
			<thead><tr><th>Key</th><th>Role</th><th>Format</th><th>Links to</th></tr></thead>
			<tbody>
				{#each view.keys as k (k.code)}
					<tr>
						<td class="mono"><b>{k.code}</b></td>
						<td class="small">
							{#each k.roles as r (r)}<span class="chip">{r.replace('ECONOMIC_KEY', 'economic key').replace('EVENT_KEY', 'event key').replace('ITEM_KEY', 'item key of').replace('LINK_KEY', 'link in')}</span> {/each}
						</td>
						<td class="mono small">{k.pattern ?? 'any'}{k.caseInsensitive ? ' (any case)' : ''}{k.scope?.length ? ` within ${k.scope.join('+')}` : ''}</td>
						<td class="small">{(k.links ?? []).map((l) => `${l.to} (${l.cardinality.toLowerCase().replaceAll('_', '-')})`).join(', ') || '—'}</td>
					</tr>
				{/each}
			</tbody>
		</table>
		<p class="small muted econ">Economic event key: <span class="mono">{view.event.code}:{view.event.economicKey.map((k) => `<${k}>`).join(':')}</span> — the same economic event posts once, whichever source sends it.</p>
	</section>

	<section class="card">
		<h3>Dates and accounting period</h3>
		{#if view.datePolicy}
			<dl class="kv">
				<dt>Date policy</dt><dd class="mono">{view.datePolicy.code}</dd>
				<dt>Books on</dt><dd>{view.datePolicy.bookDate.replace('_', ' ').toLowerCase()}{view.datePolicy.businessDayRule === 'NEXT_BUSINESS_DAY' ? ', moved to the next business day' : ''}</dd>
				<dt>Calendar</dt><dd>{view.calendar?.code ?? view.datePolicy.calendar} <span class="muted">{view.calendar?.timezone ?? ''}{view.calendar?.holidays ? ` · ${view.calendar.holidays.length} holidays` : ''}</span></dd>
				<dt>Fiscal year</dt><dd>starts in month {view.datePolicy.fiscalYearStartMonth ?? 1}</dd>
				<dt>Advance</dt><dd>{view.datePolicy.advance ? 'yes: effective after the book period → BOOKING_CLASS A' : 'no'}</dd>
				<dt>Lookups dated by</dt><dd>{(view.datePolicy.lookupDate ?? 'EFFECTIVE_DATE').replace('_', ' ').toLowerCase()}</dd>
				<dt>Period close</dt><dd>{view.datePolicy.closeAfterBusinessDay ? `after business day ${view.datePolicy.closeAfterBusinessDay} of the next month` : 'not set'}</dd>
				<dt>Late events</dt><dd>{(view.datePolicy.latePolicy ?? 'NEXT_OPEN_PERIOD').replaceAll('_', ' ').toLowerCase()}</dd>
			</dl>
		{:else}
			<p class="muted">No date policy: the transaction date books the event.</p>
		{/if}
	</section>

	<section class="card">
		<h3>Dimensions</h3>
		<table class="data">
			<thead><tr><th>Dimension</th><th>Comes from</th><th>Values</th><th>Used</th></tr></thead>
			<tbody>
				{#each view.dimensions as d (d.code)}
					<tr>
						<td><span class="mono"><b>{d.code}</b></span><div class="small muted">{d.label}</div></td>
						<td class="small">
							{#if d.carried}the event{:else}lookup <span class="mono">{d.derivedBy}</span> (calculated, not collected){/if}
							{#each d.normalizations ?? [] as nz (nz.sourceSystem)}
								<div class="muted">{nz.sourceSystem} sends {Object.entries(nz.map).map(([k, v]) => `${k}→${v}`).join(', ')}</div>
							{/each}
						</td>
						<td class="small mono">{d.values?.join(', ') ?? 'any'}</td>
						<td>{#if d.required}<span class="badge ok">needed</span>{:else}<span class="badge off">not used</span>{/if}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</section>

	<section class="card">
		<h3>Measures</h3>
		<p class="small muted">Amounts the source supplies, from the measure catalog (US-006).</p>
		<table class="data">
			<thead><tr><th>Measure</th><th>Meaning</th><th>Grain</th><th>Used</th></tr></thead>
			<tbody>
				{#each view.measures as m (m.list + m.code)}
					<tr>
						<td class="mono"><b>{m.code}</b></td>
						<td class="small">{m.name}<div class="muted">{m.dataType.toLowerCase()}, {m.sign.toLowerCase()} · {m.origin.toLowerCase()}</div></td>
						<td class="small">{m.list ? `each ${m.list} item` : 'event'}</td>
						<td>{#if m.required}<span class="badge ok">needed</span>{:else}<span class="badge off">not used</span>{/if}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</section>

	<section class="card">
		<div class="row">
			<h3 class="grow">Formulas (calculated measures)</h3>
			{#if ctx.canEdit && !editing}<button class="btn" onclick={() => edit()}>Add formula</button>{/if}
		</div>
		<p class="small muted">Calculated by GL from the measures, never collected (US-007).</p>
		{#each view.derived as d (d.code)}
			<div class="formula">
				<div class="row">
					<span class="mono"><b>{d.code}</b></span>
					<span class="muted small">=</span>
					<Formula src={d.formula} {known} />
					<span class="spacer"></span>
					{#if d.required}<span class="badge ok">needed</span>{:else}<span class="badge off">not used</span>{/if}
					{#if ctx.canEdit && !editing}<button class="btn" onclick={() => edit(d.code)}>Edit</button>{/if}
				</div>
				<div class="small muted">
					{d.resultType.toLowerCase()} · reads {d.inputs.join(', ')} · used by {usedBy('derived:' + d.code).join(', ') || 'nothing'}
				</div>
			</div>
		{:else}
			<p class="muted small">No formulas for this event.</p>
		{/each}
		{#if !ctx.canEdit && ctx.isEditor}
			<p class="small muted">To change a formula, <button class="btn link" onclick={() => ctx.startDraft({ ...base })}>start a draft from v{ctx.version.version}</button>.</p>
		{/if}
		{#if editing}
			<div class="editor">
				<label class="lbl">Code <input class="field mono" bind:value={editing.dm.code} disabled={!!editing.original} /></label>
				<label class="lbl">Formula <input class="field mono" bind:value={editing.dm.formula} placeholder="e.g. PREMIUM + TAX" /></label>
				<label class="lbl"
					>Result
					<select class="field" bind:value={editing.dm.resultType}>
						{#each ['AMOUNT', 'NUMBER', 'BOOL', 'STRING', 'DATE'] as t (t)}<option value={t}>{t.toLowerCase()}</option>{/each}
					</select>
				</label>
				<label class="lbl">Description <input class="field" bind:value={editing.dm.description} /></label>
				{#if check.length}<Findings findings={check} />{:else if editing.dm.formula}<div class="notice ok small">The formula checks against this event.</div>{/if}
				{#if saveError}<div class="notice bad" role="alert">{saveError}</div>{/if}
				<div class="row">
					<span class="spacer"></span>
					<button class="btn" onclick={() => (editing = null)}>Cancel</button>
					<button class="btn primary" disabled={!editing.dm.formula.trim() || saving} onclick={() => (asking = true)}>Save to draft v{ctx.version.version}</button>
				</div>
			</div>
		{/if}
	</section>

	{#if view.event.lists?.length}
		<section class="card">
			<h3>Lists (one-to-many)</h3>
			{#each view.event.lists as l (l.code)}
				<dl class="kv">
					<dt>List</dt><dd class="mono"><b>{l.code}</b></dd>
					<dt>One item per</dt><dd class="mono">{l.itemKey.join(' + ')}</dd>
					<dt>Linked by</dt><dd class="mono">{l.keys?.join(', ') || '—'}</dd>
					<dt>Item measures</dt><dd class="mono">{l.measures?.join(', ') || '—'}</dd>
				</dl>
				<p class="small muted">Item measures are carried once per item and never multiplied into the event.</p>
			{/each}
		</section>
	{/if}

	{#if view.lookups.length}
		<section class="card">
			<h3>Lookups</h3>
			{#each view.lookups as lk (lk.code)}
				<p class="small"><span class="mono"><b>{lk.code}</b></span>: {lk.output} from {lk.input}</p>
				<table class="data">
					<thead><tr><th>{lk.input}</th><th>{lk.output}</th><th>Effective</th></tr></thead>
					<tbody>
						{#each lk.entries as e, i (i)}
							<tr><td class="mono">{e.key}</td><td class="mono">{e.value}</td><td class="small">{e.effectiveFrom ?? '…'} → {e.effectiveTo ?? 'open'}</td></tr>
						{/each}
					</tbody>
				</table>
			{/each}
		</section>
	{/if}

	{#if view.findings.length}
		<section class="card">
			<h3>Findings for this event</h3>
			<Findings findings={view.findings} />
		</section>
	{/if}
</div>

{#if asking && editing}
	<ReasonDialog
		title="Save formula {editing.dm.code} to draft v{ctx.version.version}"
		verb="Save"
		detail="The draft is saved and must be validated and approved by someone else before it applies."
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = false)}
	/>
{/if}

<style>
	.chain h3 {
		margin-bottom: 12px;
	}
	.steps {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 0;
		counter-reset: s;
	}
	.steps li {
		position: relative;
		display: flex;
	}
	.steps li + li::before {
		content: '';
		position: absolute;
		left: -8px;
		top: 50%;
		width: 12px;
		height: 12px;
		border-top: 2px solid var(--gl-border-strong);
		border-right: 2px solid var(--gl-border-strong);
		transform: translateY(-50%) rotate(45deg);
		z-index: 1;
		background: none;
	}
	.steps button {
		font: inherit;
		color: inherit;
		text-align: left;
		display: grid;
		gap: 2px;
		width: 100%;
		margin: 0 8px;
		padding: 10px 12px;
		border-radius: 8px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface-2);
		cursor: pointer;
	}
	.steps button:hover {
		border-color: var(--gl-accent);
	}
	.n {
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		font-size: 11px;
		font-weight: 700;
	}
	.t {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
		font-weight: 600;
	}
	.v {
		font-weight: 650;
		word-break: break-word;
	}
	.s {
		font-size: 11.5px;
		color: var(--gl-muted);
	}
	.sections {
		margin-top: 14px;
	}
	.econ {
		margin: 8px 0 0;
	}
	.grow {
		flex: 1;
	}
	.formula {
		padding: 8px 0;
		border-top: 1px solid var(--gl-border);
	}
	.editor {
		display: grid;
		gap: 8px;
		margin-top: 10px;
		padding: 12px;
		border-radius: 8px;
		border: 1px dashed var(--gl-accent);
		background: var(--gl-surface-2);
	}
</style>
