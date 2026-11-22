<script lang="ts">
	// Edits one posting rule of a draft. The edited rule is placed into a copy of the stored
	// content and checked by the server as you type (POST …/check: the same validation that
	// gates approval); saving PUTs the whole content with the digest it was read at and a
	// reason for the audit trail.
	import { untrack } from 'svelte';
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { AccountingClass, EventView, ItemChange, PostingRule } from '../lib/studio';
	import type { Finding } from '../lib/types';
	import { clone, under, withRule } from '../lib/content';
	import Findings from '../components/Findings.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let { ctx, view, rule, original, onclose }: { ctx: Ctx; view: EventView; rule: PostingRule; original?: string; onclose: (saved?: string) => void } =
		$props();

	// The editor starts from the rule it was opened on (it is re-created for another rule).
	let r = $state<PostingRule>(untrack(() => clone(rule)));
	let active = $state(untrack(() => rule.active !== false));
	let findings = $state<Finding[]>([]);
	let others = $state(0);
	let changes = $state<ItemChange[]>([]);
	let checking = $state(false);
	let asking = $state(false);
	let saving = $state(false);
	let error = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	const classes = $derived((ctx.content.accountingClasses ?? []) as AccountingClass[]);
	const lists = $derived(view.event.lists ?? []);
	const names = $derived([
		...view.measures.map((m) => (m.list ? `${m.list}.${m.code}` : m.code)),
		...view.derived.map((d) => d.code),
		...view.dimensions.map((d) => d.code),
		'TRANSACTION_DATE',
		'EFFECTIVE_DATE'
	]);
	const candidate = $derived.by(() => {
		const x = clone(r);
		x.active = active ? undefined : false;
		return withRule(ctx.content, x, original);
	});
	const index = $derived(((candidate.postingRules ?? []) as PostingRule[]).findIndex((x) => x.code === r.code.trim()));

	$effect(() => {
		const c = candidate;
		const i = index;
		clearTimeout(timer);
		checking = true;
		timer = setTimeout(() => {
			ctx.api.check(ctx.version.configId, ctx.version.version, c).then(
				(res) => {
					findings = under(res.findings, `postingRules/${i}`);
					others = res.findings.filter((f) => f.severity === 'error' && !f.path.startsWith(`postingRules/${i}`)).length;
					changes = res.changes;
					checking = false;
				},
				(e: ApiError) => {
					findings = [{ path: '', code: e.code, severity: 'error', message: e.message }];
					checking = false;
				}
			);
		}, 350);
	});

	function addLine() {
		r.lines = [...r.lines, { code: `LINE_${r.lines.length + 1}`, class: '', side: r.lines.filter((l) => l.side === 'D').length > r.lines.length / 2 ? 'C' : 'D', amount: '' }];
	}
	function removeLine(i: number) {
		r.lines = r.lines.filter((_, j) => j !== i);
	}
	function move(i: number, d: number) {
		const ls = [...r.lines];
		const [x] = ls.splice(i, 1);
		ls.splice(i + d, 0, x);
		r.lines = ls;
	}

	async function save(reason: string) {
		saving = true;
		error = '';
		try {
			await ctx.save(candidate, reason);
			asking = false;
			onclose(r.code.trim());
		} catch (e) {
			const err = e as ApiError;
			error = err.code === 'STALE_VERSION' ? 'Someone else saved this draft meanwhile. Reload the page and reapply your change.' : `${err.code}: ${err.message}`;
			asking = false;
		} finally {
			saving = false;
		}
	}

	const errors = $derived(findings.filter((f) => f.severity === 'error').length);
	const lineFindings = (i: number) => under(findings, `postingRules/${index}/lines/${i}`);
	const ruleFindings = $derived(findings.filter((f) => !f.path.includes('/lines/')));
</script>

<datalist id="gl-names">
	{#each names as n (n)}<option value={n}></option>{/each}
</datalist>

<section class="card editor">
	<div class="row head">
		<div class="grow">
			<div class="kind">{original ? 'Editing posting rule' : 'New posting rule'} · draft v{ctx.version.version}</div>
			<h2 class="mono">{r.code || '—'}</h2>
		</div>
		<span class="small muted">{checking ? 'Checking…' : errors ? `${errors} problem${errors === 1 ? '' : 's'} in this rule` : 'This rule checks out'}</span>
		<button class="btn" onclick={() => onclose()}>Cancel</button>
		<button class="btn primary" disabled={saving || !r.code.trim()} onclick={() => (asking = true)}>Save to draft</button>
	</div>

	<div class="fields">
		<label class="lbl">Rule code <input class="field mono" bind:value={r.code} disabled={!!original} /></label>
		<label class="lbl">Rank <input class="field" type="number" bind:value={r.rank} /></label>
		<label class="lbl wide">Description <input class="field" bind:value={r.description} placeholder="What this rule books, in business words" /></label>
		<label class="lbl wide">Filter (optional) <input class="field mono" list="gl-names" bind:value={r.filter} placeholder="e.g. LOB == 'MOTOR' — empty: every {view.event.code}" /></label>
		<label class="lbl">Effective from (optional) <input class="field" type="date" bind:value={r.effectiveFrom} /></label>
		<label class="lbl">Effective to (optional) <input class="field" type="date" bind:value={r.effectiveTo} /></label>
		<label class="lbl"
			>Journal type (optional)
			<select class="field" bind:value={r.journalType}>
				<option value={undefined}>—</option>
				{#each (ctx.content.journalTypes ?? []) as { code: string; description?: string; reverse?: string }[] as j (j.code)}<option value={j.code}
						>{j.code}{j.description ? ` — ${j.description}` : ''}{j.reverse === 'NEXT_PERIOD' ? ' (reversed next period)' : ''}</option
					>{/each}
			</select>
		</label>
		<label class="toggle"><input type="checkbox" bind:checked={active} /> Active (a deactivated rule is kept for the record but never applied)</label>
	</div>
	{#if ruleFindings.length}<Findings findings={ruleFindings} />{/if}

	<h3 class="lh">Debit and credit lines</h3>
	<div class="scroll">
		<table class="data lines">
			<thead>
				<tr>
					<th></th>
					<th>Line</th>
					<th>Side</th>
					<th>Accounting class</th>
					<th>Amount formula</th>
					<th>Per item of</th>
					<th>Legal entity</th>
					<th>Only when</th>
					<th>Note</th>
					<th></th>
				</tr>
			</thead>
			<tbody>
				{#each r.lines as l, i (i)}
					<tr>
						<td class="mv">
							<button class="btn link" aria-label="Move line up" disabled={i === 0} onclick={() => move(i, -1)}>▲</button>
							<button class="btn link" aria-label="Move line down" disabled={i === r.lines.length - 1} onclick={() => move(i, 1)}>▼</button>
						</td>
						<td><input class="field mono w8" bind:value={l.code} aria-label="Line code" /></td>
						<td>
							<select class="field" bind:value={l.side} aria-label="Side">
								<option value="D">Debit</option>
								<option value="C">Credit</option>
							</select>
						</td>
						<td>
							<select class="field" bind:value={l.class} aria-label="Accounting class">
								<option value="" disabled>choose…</option>
								{#each classes as c (c.code)}<option value={c.code}>{c.code} · {c.name}</option>{/each}
							</select>
						</td>
						<td><input class="field mono w12" list="gl-names" bind:value={l.amount} aria-label="Amount formula" placeholder="e.g. PREMIUM" /></td>
						<td>
							<select class="field" bind:value={l.expandBy} aria-label="Expand by list">
								<option value={undefined}>— (one leg)</option>
								{#each lists as x (x.code)}<option value={x.code}>{x.code}</option>{/each}
							</select>
						</td>
						<td><input class="field mono w8" list="gl-names" bind:value={l.entity} aria-label="Legal entity" placeholder="COMPANY" /></td>
						<td><input class="field mono w10" list="gl-names" bind:value={l.when} aria-label="Condition" placeholder="always" /></td>
						<td><input class="field w10" bind:value={l.explanation} aria-label="Explanation" /></td>
						<td><button class="btn danger" aria-label="Remove line {l.code}" onclick={() => removeLine(i)} disabled={r.lines.length <= 2}>Remove</button></td>
					</tr>
					{#if lineFindings(i).length}
						<tr class="lf"><td></td><td colspan="9"><Findings findings={lineFindings(i)} /></td></tr>
					{/if}
				{/each}
			</tbody>
		</table>
	</div>
	<div class="row">
		<button class="btn" onclick={addLine}>Add line</button>
		<span class="small muted">Names you can use: measures, formulas, dimensions and dates of {view.event.code} (type to see suggestions).</span>
	</div>

	<div class="grid2 after">
		<div>
			<h4>What will change in draft v{ctx.version.version}</h4>
			{#if changes.length}
				<ul class="small">
					{#each changes as c (c.section + c.key)}
						<li><span class="mono">{c.section} {c.key}</span>: {c.change.toLowerCase()}{c.fields.length ? ` (${c.fields.join(', ')})` : ''}</li>
					{/each}
				</ul>
			{:else}
				<p class="small muted">No change yet.</p>
			{/if}
		</div>
		<div>
			<h4>Elsewhere in the draft</h4>
			<p class="small muted">
				{others ? `${others} blocking finding(s) outside this rule.` : 'No blocking findings outside this rule.'} After saving, validate the draft and have
				it approved by someone who did not edit it; then check Change impact for the fields, mappings and journals it affects.
			</p>
		</div>
	</div>
	{#if error}<div class="notice bad" role="alert">{error}</div>{/if}
</section>

{#if asking}
	<ReasonDialog
		title="Save {r.code} to draft v{ctx.version.version}"
		verb="Save"
		detail={errors ? `This rule still has ${errors} problem(s); the draft can be saved but not validated until they are fixed.` : 'Saved changes are audited with your reason.'}
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = false)}
	/>
{/if}

<style>
	.editor {
		margin-top: 18px;
		border-color: var(--gl-accent);
	}
	.head {
		align-items: flex-end;
		margin-bottom: 12px;
	}
	.grow {
		flex: 1;
	}
	.kind {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--gl-accent);
		font-weight: 700;
	}
	h2 {
		margin: 2px 0 0;
		font-size: 20px;
	}
	.fields {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 10px;
		margin-bottom: 10px;
	}
	.wide {
		grid-column: span 2;
	}
	@media (max-width: 640px) {
		.wide {
			grid-column: auto;
		}
	}
	.toggle {
		display: flex;
		gap: 6px;
		align-items: center;
		font-size: 13px;
		grid-column: 1 / -1;
	}
	.lh {
		margin-top: 14px;
	}
	.lines td {
		vertical-align: middle;
	}
	.mv {
		white-space: nowrap;
	}
	.w8 {
		width: 8em;
	}
	.w10 {
		width: 10em;
	}
	.w12 {
		width: 13em;
	}
	.lf td {
		padding-top: 0;
	}
	.after {
		margin-top: 14px;
		padding-top: 10px;
		border-top: 1px solid var(--gl-border);
	}
	.after ul {
		margin: 0;
		padding-left: 18px;
	}
</style>
