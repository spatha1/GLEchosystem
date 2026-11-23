<script lang="ts">
	import { untrack } from 'svelte';
	// Step 3 — account assignment rules: class + conditions → the client's account. Read from the
	// client's assignment matrix (one rule per row and account column, every original value kept
	// with the rule), entered by hand, or accepted from the assistant. Rows that cannot be read
	// are listed with the reason, never guessed.
	import type { S1 } from './ctx';
	import type { Content } from '../../lib/studio';
	import { clone } from '../../lib/content';
	import type { Suggestion } from '../api';
	import { codeOf, loadFile, sheetId, sheetLabel, type LoadedSheet } from '../files';
	import { accounts, applyMatrix, classes, detectMatrix, overlaps, readMatrix, rules, type MatrixColumns, type Rule } from './model';
	import ReasonDialog from '../../components/ReasonDialog.svelte';
	import AIPanel from '../AIPanel.svelte';
	import RuleTester from './RuleTester.svelte';

	let { ctx }: { ctx: S1 } = $props();

	let work = $state<Content>(untrack(() => clone(ctx.content)));
	let changes = $state<string[]>([]);
	let error = $state('');
	let done = $state('');
	let asking = $state(false);
	let saving = $state(false);
	let sheetKey = $state('');
	let cols = $state<MatrixColumns | null>(null);
	let filter = $state('');
	let edit = $state<Rule | null>(null);
	let editIsNew = $state(false);

	const sheets = $derived(ctx.files.flatMap((f) => f.sheets));
	const sheet = $derived(sheets.find((s) => sheetLabel(s) === sheetKey) ?? null);
	$effect(() => {
		if (!sheetKey) {
			const a = sheets.find((s) => /assign/i.test(`${s.file} ${s.sheet}`));
			if (a) pick(sheetLabel(a));
		}
	});
	function pick(k: string) {
		sheetKey = k;
		const s = sheets.find((x) => sheetLabel(x) === k);
		cols = s ? detectMatrix(s, ctx.systems[0] ?? 'SOURCE') : null;
	}
	async function addFiles(list: FileList | null) {
		const out = [];
		for (const f of Array.from(list ?? [])) {
			try {
				out.push(await loadFile(f));
			} catch (e) {
				error = (e as Error).message;
			}
		}
		ctx.addFiles(out);
		const s = out.flatMap((f) => f.sheets)[0];
		if (s) pick(sheetLabel(s));
	}

	const plan = $derived(sheet && cols ? readMatrix(work, sheet, cols, ctx.me.actor) : null);
	function importMatrix() {
		if (!plan || !sheet) return;
		// rules already entered for rows of this sheet (by hand or from the assistant) are kept
		const kept = rules(work).filter((r) => r.source?.file === plan.file.id && !r.code.startsWith(`AA_${plan.file.id}_`));
		work = applyMatrix(work, plan);
		if (kept.length) work = { ...work, accountAssignmentRules: [...rules(work), ...kept.filter((k) => !rules(work).some((r) => r.code === k.code))] };
		changes = [...changes, plan.rules.length ? `${plan.rules.length} rules from ${sheetLabel(sheet)} (${plan.skipped.length} rows not read)` : `${sheetLabel(sheet)} recorded: ${plan.skipped.length} rows to enter by hand`];
	}
	// a rule entered by hand can name the row of the matrix it comes from
	function linkRow(n: number) {
		if (!edit || !sheet) return;
		if (!n) {
			delete edit.source;
			return;
		}
		edit.source = { file: sheetId(sheet), sheet: sheet.sheet || undefined, row: n, values: Object.fromEntries(sheet.header.map((h, i) => [h, (sheet.rows[n - 1]?.[i] ?? '').trim()]).filter(([h, v]) => h && v)) };
	}

	const all = $derived(rules(work));
	const shown = $derived(filter ? all.filter((r) => r.class === filter) : all);
	const byClass = $derived([...new Set(shown.map((r) => r.class))]);
	const accountName = (code: string) => accounts(work).find((a) => a.code === code)?.name;
	const dup = $derived(overlaps(work));
	const fileOf = (id?: string) => ((work.sourceFiles ?? []) as { id: string; name: string; sheet?: string }[]).find((f) => f.id === id);
	const condKeys = $derived([...new Set([...all.flatMap((r) => Object.keys(r.match ?? {})), ...((work.dimensions ?? []) as { code: string }[]).map((d) => d.code)])].sort());

	function saveRule() {
		if (!edit) return;
		const r: Rule = { ...edit, code: edit.code.trim().toUpperCase() };
		const m = Object.fromEntries(Object.entries(r.match ?? {}).filter(([k, v]) => k.trim() && String(v).trim()));
		if (Object.keys(m).length) r.match = m;
		else delete r.match;
		if (r.priority === undefined || (r.priority as unknown) === '' || Number.isNaN(Number(r.priority))) delete r.priority;
		else r.priority = Number(r.priority);
		for (const k of ['effectiveFrom', 'effectiveTo', 'description'] as const) if (!r[k]) delete r[k];
		const c = clone(work);
		const list = rules(c);
		if (editIsNew && list.some((x) => x.code === r.code)) {
			error = `Rule ${r.code} already exists.`;
			return;
		}
		c.accountAssignmentRules = editIsNew ? [...list, r] : list.map((x) => (x.code === r.code ? r : x));
		// a condition that is not a dimension yet becomes one (any value)
		const dims = (c.dimensions ?? []) as any[];
		for (const k of Object.keys(r.match ?? {})) if (!dims.some((d) => d.code === k) && !['ENTITY', 'CURRENCY', 'EVENT_TYPE', 'BOOKING_CLASS'].includes(k)) dims.push({ code: k, custom: true, open: true, description: 'Assignment condition' });
		if (dims.length) c.dimensions = dims;
		work = c;
		changes = [...changes, `${editIsNew ? 'added' : 'changed'} ${r.code}`];
		edit = null;
	}
	function removeRule(code: string) {
		const c = clone(work);
		c.accountAssignmentRules = rules(c).filter((r) => r.code !== code);
		work = c;
		changes = [...changes, `removed ${code}`];
		edit = null;
	}
	let mEdit = $state<{ k: string; v: string }[]>([]);
	function openRule(r: Rule | null) {
		editIsNew = !r;
		edit = r ? clone(r) : { code: `AA_${(all.length + 1).toString().padStart(3, '0')}`, class: filter || classes(work)[0]?.code || '', account: '' };
		mEdit = Object.entries(edit.match ?? {}).map(([k, v]) => ({ k, v }));
		if (!mEdit.length) mEdit = [{ k: '', v: '' }];
	}
	$effect(() => {
		if (edit) edit.match = Object.fromEntries(mEdit.filter((x) => x.k).map((x) => [x.k, x.v]));
	});

	async function save(reason: string) {
		saving = true;
		error = '';
		try {
			await ctx.save(work, reason);
			done = `${changes.length} change${changes.length === 1 ? '' : 's'} saved.`;
			changes = [];
			asking = false;
		} catch (e) {
			error = (e as Error).message;
			asking = false;
		} finally {
			saving = false;
		}
	}

	function aiAccept(s: Suggestion) {
		const d = s.data;
		if (s.kind === 'rule') {
			const row = Number(d.sourceRow) || undefined;
			const r: Rule = { code: `AA_AI_${(all.length + 1).toString().padStart(3, '0')}`, class: String(d.class ?? ''), account: String(d.account ?? ''), match: d.match ?? undefined };
			if (d.priority !== undefined && d.priority !== null && d.priority !== '') r.priority = Number(d.priority);
			if (sheet && row) r.source = { file: sheetId(sheet), sheet: sheet.sheet || undefined, row, values: Object.fromEntries(sheet.header.map((h, i) => [h, (sheet.rows[row - 1]?.[i] ?? '').trim()]).filter(([h, v]) => h && v)) };
			r.description = [s.title, d.sourceClass ? `source class ${d.sourceClass}` : ''].filter(Boolean).join(' · ');
			openRule(null);
			edit = r;
			mEdit = Object.entries(r.match ?? {}).map(([k, v]) => ({ k, v: String(v) }));
			if (!mEdit.length) mEdit = [{ k: '', v: '' }];
			return 'Opened in the rule form: check it, then add it to your changes.';
		}
		if (s.kind === 'conditions' && cols && sheet) {
			const next: Record<string, string> = {};
			for (const h of (d.columns ?? []) as string[]) if (sheet.header.includes(h)) next[h] = String(d.dimensions?.[h] ?? codeOf(h));
			cols = { ...cols, conditions: next };
			return 'Applied to the matrix columns: check the preview.';
		}
	}
</script>

<div class="with-ai">
	<div class="stack">
		{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
		{#if done}<p class="notice ok" role="status">{done}</p>{/if}
		{#if changes.length}
			<div class="card pending row">
				<b>{changes.length} unsaved change{changes.length === 1 ? '' : 's'}</b>
				<span class="small muted grow">{changes.slice(-3).join(' · ')}</span>
				<button class="btn ghost small" onclick={() => { work = clone(ctx.content); changes = []; }}>Discard</button>
				<button class="btn primary" disabled={!ctx.canEdit || saving} onclick={() => (asking = true)}>Save changes…</button>
			</div>
		{/if}

		<section class="card" aria-label="Read the assignment matrix">
			<div class="row">
				<h2 class="sec grow">Read the client’s assignment matrix</h2>
				<label class="btn small">Choose file<input class="hide" type="file" accept=".xlsx,.csv" aria-label="Assignment matrix" onchange={(e) => addFiles((e.currentTarget as HTMLInputElement).files)} disabled={!ctx.canEdit} /></label>
				{#if sheets.length}
					<select class="field" value={sheetKey} onchange={(e) => pick((e.currentTarget as HTMLSelectElement).value)} aria-label="Sheet"><option value="">Choose a sheet…</option>{#each sheets as s (sheetLabel(s))}<option value={sheetLabel(s)}>{sheetLabel(s)}</option>{/each}</select>
				{/if}
			</div>
			{#if sheet && cols}
				<div class="grid2">
					<label class="lbl">Source class column
						<select class="field" bind:value={cols.sourceClass}><option value={undefined}>— none —</option>{#each sheet.header as h, hi (hi)}<option value={h}>{h}</option>{/each}</select></label
					>
					<label class="lbl">Its source system <input class="field mono" bind:value={cols.system} /></label>
					<label class="lbl">Or a column with this client’s class codes
						<select class="field" bind:value={cols.clientClass}><option value={undefined}>— none —</option>{#each sheet.header as h, hi (hi)}<option value={h}>{h}</option>{/each}</select></label
					>
					<label class="lbl">Priority column
						<select class="field" bind:value={cols.priority}><option value={undefined}>— none —</option>{#each sheet.header as h, hi (hi)}<option value={h}>{h}</option>{/each}</select></label
					>
				</div>
				<h3 class="h3">Account columns</h3>
				{#each cols.accounts as a, i (i)}
					<div class="row">
						<select class="field" bind:value={a.header}>{#each sheet.header as h, hi (hi)}<option value={h}>{h}</option>{/each}</select>
						<span class="small muted">for</span>
						<input class="field mono" style="width:130px" value={Object.keys(a.when)[0] ?? ''} placeholder="ENTITY (optional)" oninput={(e) => {
								const k = (e.currentTarget as HTMLInputElement).value.toUpperCase();
								const v = Object.values(a.when)[0] ?? '';
								a.when = k ? { [k]: v } : {};
							}} aria-label="Account column condition" />
						<span class="small muted">=</span>
						<input class="field mono" style="width:120px" value={Object.values(a.when)[0] ?? ''} placeholder="value" oninput={(e) => {
								const k = Object.keys(a.when)[0];
								if (k) a.when = { [k]: (e.currentTarget as HTMLInputElement).value };
							}} aria-label="Account column condition value" />
						<button class="btn ghost small" onclick={() => (cols!.accounts = cols!.accounts.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<button class="btn ghost small" onclick={() => (cols!.accounts = [...cols!.accounts, { header: sheet!.header[0], when: {} }])}>+ account column</button>
				<h3 class="h3">Condition columns <span class="small muted">(each becomes a condition; * or ALL means any value, an empty cell means it does not matter)</span></h3>
				<div class="conds">
					{#each sheet.header.filter((h) => h !== cols!.sourceClass && h !== cols!.clientClass && h !== cols!.priority && !cols!.accounts.some((a) => a.header === h)) as h, hi (hi)}
						<label class="cond" class:on={cols.conditions[h]}>
							<input type="checkbox" checked={!!cols.conditions[h]} onchange={(e) => {
									const next = { ...cols!.conditions };
									if ((e.currentTarget as HTMLInputElement).checked) next[h] = codeOf(h);
									else delete next[h];
									cols!.conditions = next;
								}} />
							<span class="small">{h}</span>
							{#if cols.conditions[h]}<input class="field mono small" bind:value={cols.conditions[h]} aria-label={`Condition code for ${h}`} />{/if}
						</label>
					{/each}
				</div>
				{#if plan}
					<div class="planbar row">
						<span class="pill ok">{plan.rules.length} rules</span>
						<span class="pill {plan.skipped.length ? 'warn' : ''}">{plan.skipped.length} rows not read</span>
						{#if plan.newDimensions.length}<span class="small muted">new conditions: {plan.newDimensions.join(', ')}</span>{/if}
						<span class="grow"></span>
						<button class="btn primary" disabled={!ctx.canEdit || (!plan.rules.length && !plan.skipped.length)} onclick={importMatrix}>{plan.rules.length ? 'Add to my changes' : 'Record the file and its rows'}</button>
					</div>
					{#if plan.skipped.length}
						<details open={plan.rules.length === 0}><summary class="small">Rows not read, and why</summary>
							<div class="tw"><table class="grid"><tbody>{#each plan.skipped as k, ki (ki)}<tr><td class="small">row {k.row}</td><td class="small">{k.reason}</td><td class="small muted">{sheet.rows[k.row - 1]?.slice(0, 4).join(' | ')}</td></tr>{/each}</tbody></table></div>
							<p class="small muted">Enter these by hand below, or ask the assistant to turn them into rules with the row as evidence.</p>
						</details>
					{/if}
				{/if}
			{:else}
				<p class="muted small">Choose the client’s assignment workbook (or a sheet brought in step 1). Every source field, wildcard and row is kept with the rule it gives.</p>
			{/if}
		</section>

		<section class="card" aria-label="Assignment rules">
			<div class="row">
				<h2 class="sec grow">Assignment rules <span class="pill">{all.length}</span></h2>
				<select class="field" bind:value={filter} aria-label="Class filter"><option value="">All classes</option>{#each classes(work) as c, ci (ci)}<option value={c.code}>{c.code}</option>{/each}</select>
				<button class="btn" disabled={!ctx.canEdit || !classes(work).length} onclick={() => openRule(null)}>+ Add rule</button>
			</div>
			{#if dup.length}<p class="notice warn small">{dup.length} pair{dup.length === 1 ? '' : 's'} of rules with identical conditions: {dup.slice(0, 4).map(([a, b]) => `${a.code} / ${b.code}`).join(', ')} — the engine would refuse to choose between them.</p>{/if}
			{#if !all.length}<p class="muted">No rules yet. Each class needs at least one rule saying which account it goes to.</p>{/if}
			{#each byClass as c (c)}
				<h3 class="h3"><span class="mono">{c}</span> <span class="small muted">{classes(work).find((x) => x.code === c)?.name ?? ''} · {shown.filter((r) => r.class === c).length} rules</span></h3>
				<div class="tw">
					<table class="grid">
						<thead><tr><th>Rule</th><th>Conditions</th><th>Account</th><th>Priority</th><th>Effective</th><th>Source</th></tr></thead>
						<tbody>
							{#each shown.filter((r) => r.class === c) as r, ri (ri)}
								<tr class="click" onclick={() => openRule(r)}>
									<td class="mono small">{r.code}</td>
									<td>{#each Object.entries(r.match ?? {}) as [k, v] (k)}<span class="pill">{k} = {v === '*' ? 'any' : v}</span> {:else}<span class="small muted">any line</span>{/each}</td>
									<td><b class="mono">{r.account}</b> <span class="small {accountName(r.account) ? 'muted' : 'bad'}">{accountName(r.account) ?? 'not in the chart of accounts'}</span></td>
									<td>{r.priority ?? ''}</td>
									<td class="small">{r.effectiveFrom ?? ''}{r.effectiveTo ? ` → ${r.effectiveTo}` : ''}</td>
									<td class="small muted" title={r.source?.values ? Object.entries(r.source.values).map(([k, v]) => `${k}: ${v}`).join('\n') : ''}>{r.source?.file ? `${fileOf(r.source.file)?.name ?? r.source.file} › row ${r.source.row}` : 'by hand'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/each}
		</section>

		{#if edit}
			<section class="card selcard" aria-label="Rule">
				<h2 class="sec">{editIsNew ? 'New rule' : `Rule ${edit.code}`}</h2>
				<div class="grid2">
					<label class="lbl">Rule code <input class="field mono" bind:value={edit.code} disabled={!editIsNew} /></label>
					<label class="lbl">Class <select class="field" bind:value={edit.class}>{#each classes(work) as c, ci (ci)}<option value={c.code}>{c.code} · {c.name}</option>{/each}</select></label>
					<label class="lbl">Account
						<input class="field mono" list="ws-accounts" bind:value={edit.account} />
						<datalist id="ws-accounts">{#each accounts(work) as a, ai (ai)}<option value={a.code}>{a.name}</option>{/each}</datalist></label
					>
					<label class="lbl">Priority (lower wins) <input class="field" type="number" bind:value={edit.priority} /></label>
					<label class="lbl">Effective from <input class="field" type="date" bind:value={edit.effectiveFrom} /></label>
					<label class="lbl">Effective to <input class="field" type="date" bind:value={edit.effectiveTo} /></label>
				</div>
				<h3 class="h3">Conditions <span class="small muted">(* = any present value)</span></h3>
				{#each mEdit as m, i (i)}
					<div class="row">
						<input class="field mono" list="ws-ckeys" bind:value={m.k} placeholder="CONDITION" oninput={() => (m.k = m.k.toUpperCase())} aria-label="Condition" />
						<span>=</span>
						<input class="field mono" bind:value={m.v} placeholder="value or *" aria-label="Condition value" />
						<button class="btn ghost small" onclick={() => (mEdit = mEdit.filter((_, j) => j !== i))}>Remove</button>
					</div>
				{/each}
				<datalist id="ws-ckeys">{#each [...new Set(['ENTITY', 'CURRENCY', ...condKeys])] as k (k)}<option value={k}></option>{/each}</datalist>
				<button class="btn ghost small" onclick={() => (mEdit = [...mEdit, { k: '', v: '' }])}>+ condition</button>
				<label class="lbl desc">Description <input class="field" bind:value={edit.description} /></label>
				{#if sheet}
					<label class="lbl desc">From row of {sheetLabel(sheet)} (optional: keeps the row with the rule)
						<select class="field" value={edit.source?.file === sheetId(sheet) ? edit.source?.row : 0} onchange={(e) => linkRow(Number((e.currentTarget as HTMLSelectElement).value))} aria-label="Source row">
							<option value={0}>— not from the file —</option>
							{#each sheet.rows as r, i (i)}<option value={i + 1}>row {i + 1}: {r.slice(0, 3).join(' | ').slice(0, 90)}</option>{/each}
						</select></label
					>
				{/if}
				{#if edit.source?.values}<details class="small"><summary>The source row ({fileOf(edit.source.file)?.name ?? edit.source.file} › row {edit.source.row})</summary>{#each Object.entries(edit.source.values) as [k, v] (k)}<div><span class="muted">{k}:</span> {v}</div>{/each}</details>{/if}
				<div class="row top">
					<button class="btn primary" disabled={!ctx.canEdit || !edit.code || !edit.class || !edit.account} onclick={saveRule}>{editIsNew ? 'Add to my changes' : 'Keep change'}</button>
					{#if !editIsNew}<button class="btn danger" disabled={!ctx.canEdit} onclick={() => removeRule(edit!.code)}>Remove rule</button>{/if}
					<button class="btn ghost" onclick={() => (edit = null)}>Close</button>
				</div>
			</section>
		{/if}

		<RuleTester ctx={{ ...ctx, content: ctx.content }} />
		{#if changes.length}<p class="small muted">The test runs on the saved draft: save your changes to test them.</p>{/if}
	</div>
	<AIPanel
		api={ctx.api}
		area="stage1"
		step="assignments"
		configId={ctx.version.configId}
		version={ctx.version.version}
		documents={ctx.files}
		selection={filter ? { class: filter } : undefined}
		canAct={ctx.canEdit}
		accept={aiAccept}
		examples={[
			'For each class, show all account assignment combinations and explain why they select different accounts.',
			'Turn the rows that could not be read into rules, with the source row as evidence.',
			'Which columns of the matrix are conditions?'
		]}
	/>
</div>

{#if asking}
	<ReasonDialog title="Save assignment rules" verb="Save" detail={changes.join('; ')} busy={saving} onsubmit={save} oncancel={() => (asking = false)} />
{/if}

<style>
	.grow {
		flex: 1;
	}
	.hide {
		display: none;
	}
	.pending {
		border-color: var(--warn);
		background: var(--warn-soft);
	}
	.grid2 {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: 8px 12px;
		margin: 10px 0;
	}
	.h3 {
		font-size: 13px;
		margin: 14px 0 6px;
	}
	.conds {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.cond {
		display: flex;
		align-items: center;
		gap: 6px;
		border: 1px solid var(--line);
		border-radius: 8px;
		padding: 4px 8px;
	}
	.cond.on {
		border-color: var(--accent);
		background: var(--accent-soft);
	}
	.cond .field {
		width: 160px;
		padding: 2px 6px;
	}
	.planbar {
		margin: 12px 0 6px;
		padding-top: 10px;
		border-top: 1px solid var(--line);
	}
	.bad {
		color: var(--bad);
	}
	.selcard {
		border-color: var(--accent);
	}
	.desc {
		margin-top: 10px;
	}
	.top {
		margin-top: 10px;
	}
	summary {
		cursor: pointer;
	}
</style>
