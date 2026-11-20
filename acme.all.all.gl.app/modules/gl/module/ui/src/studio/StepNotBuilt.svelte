<script lang="ts">
	// A stage of the GL flow that is not built yet. It says so plainly, names the backlog
	// story, and shows what the configuration already determines for that stage.
	import type { Ctx } from '../lib/ctx';
	import type { Account, EventView } from '../lib/studio';
	import MappingTable from './MappingTable.svelte';

	let { ctx, view, step }: { ctx: Ctx; view: EventView; step: 'mapping' | 'collection' | 'subledger' | 'reconciliation' | 'reports' } = $props();

	const about: Record<string, { title: string; story: string; text: string }> = {
		mapping: {
			title: 'Data Workbench mapping',
			story: 'US-015, US-016',
			text: 'The required source fields of this event are sent to the existing Data Workbench, which proposes source tables, columns, joins and transformations with a confidence; a reviewer accepts, corrects or rejects each proposal and a different approver approves the mapping version.'
		},
		collection: {
			title: 'Data collection',
			story: 'US-017',
			text: 'Approved mappings are compiled into read-only queries; each source record becomes a canonical event, with its identity, keys, dates, measures, dimensions and list items, and its lineage to the source rows.'
		},
		subledger: {
			title: 'Subledger',
			story: 'US-022, US-023, US-024',
			text: 'Posted journal lines update the subledger balances kept by business key: policy receivable by policy, invoice receivable by invoice, with allocations and write-offs read from the posted balance.'
		},
		reconciliation: {
			title: 'Reconciliation',
			story: 'US-028, US-038',
			text: 'Configured equations compare the subledger with the ledger and the sources, within tolerances, and explain every break.'
		},
		reports: {
			title: 'Reports and files',
			story: 'US-039, US-040',
			text: 'Configured reports, extracts, TRF layouts and finance APIs, with delivery acknowledgement and corrected outputs.'
		}
	};
	const a = $derived(about[step]);
	const accounts = $derived((ctx.content.accounts ?? []) as Account[]);
	const classes = $derived([...new Set(view.rules.filter((r) => r.isActive).flatMap((r) => r.lines.map((l) => l.line.class)))]);
	const subledgers = $derived(
		classes.flatMap((cl) =>
			accounts.filter((x) => x.class === cl && x.subledgerKey).map((x) => ({ cls: cl, account: x.code, name: x.name, key: x.subledgerKey! }))
		)
	);
	const sources = $derived(view.authority.map((x) => `${x.sourceSystem} (${x.intakeMode === 'RAW_EVENT' ? 'raw events' : 'source journals'})`));
	const count = (section: string) => ((ctx.content[section] ?? []) as unknown[]).length;
</script>

<section class="card">
	<div class="row">
		<span class="badge off">Not built yet</span>
		<h3 class="grow">{a.title}</h3>
		<span class="small muted">backlog {a.story}</span>
	</div>
	<p>{a.text}</p>
	<p class="small muted">Nothing on this step is real data: it shows what the configuration already decides for {view.event.code}, so the step can be built against it.</p>
</section>

{#if step === 'mapping'}
	<section class="card">
		<h3>Fields {view.event.code} will ask the Data Workbench for</h3>
		<MappingTable explanation={view.explanation} manifest={view.manifest} />
	</section>
{:else if step === 'collection'}
	<section class="card">
		<h3>What collection will produce for {view.event.code}</h3>
		<dl class="kv">
			<dt>From</dt><dd>{sources.join(', ') || 'no source is authoritative yet'}</dd>
			<dt>Record identity</dt><dd>source system + source record id + version (a resend is ignored, a correction replaces)</dd>
			<dt>Economic key</dt><dd class="mono">{view.event.code}:{view.event.economicKey.map((k) => `<${k}>`).join(':')}</dd>
			<dt>Fields per record</dt><dd>{view.manifest.fields.filter((f) => f.kind === 'SOURCE').length} (step 9)</dd>
			<dt>Lists</dt><dd>{(view.event.lists ?? []).map((l) => `${l.code} (one row per ${l.itemKey.join(' + ')})`).join(', ') || 'none'}</dd>
		</dl>
	</section>
{:else if step === 'subledger'}
	<section class="card">
		<h3>Subledger balances {view.event.code} will move</h3>
		{#if subledgers.length}
			<table class="data">
				<thead><tr><th>Class</th><th>Account</th><th>Kept by</th></tr></thead>
				<tbody>
					{#each subledgers as s (s.account)}
						<tr><td class="mono">{s.cls}</td><td><span class="mono">{s.account}</span> <span class="small muted">{s.name}</span></td><td class="mono">{s.key}</td></tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="muted small">None of the accounts this event posts to keeps a subledger (no subledger key configured).</p>
		{/if}
	</section>
{:else if step === 'reconciliation'}
	<section class="card">
		<h3>Reconciliation equations</h3>
		<p class="small muted">{count('reconciliationEquations')} configured in this version.</p>
	</section>
{:else}
	<section class="card">
		<h3>Output profiles</h3>
		<p class="small muted">{count('outputProfiles')} configured in this version.</p>
	</section>
{/if}

<style>
	.grow {
		flex: 1;
		margin: 0;
	}
	p {
		margin: 8px 0 0;
		max-width: 1000px;
	}
</style>
