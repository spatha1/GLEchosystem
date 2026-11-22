<script lang="ts">
	// ALTB-11 — bulk entry. Download a version as an Excel workbook (one sheet per section) or JSON;
	// import a workbook, CSV files or JSON into a draft: the page says what will be added, changed
	// or removed, the server checks the result, and it is saved with one reason (one audit entry).
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { Content, ItemChange } from '../lib/studio';
	import type { Finding } from '../lib/types';
	import { importTables, tablesFromJson, tablesFromSheets, toWorkbook, type ImportMode, type ImportResult, type Table } from '../lib/workbook';
	import { readCsv, readXlsx, writeXlsx } from '../lib/sheets';
	import ReasonDialog from '../components/ReasonDialog.svelte';
	import Findings from '../components/Findings.svelte';

	let { ctx }: { ctx: Ctx } = $props();

	let files = $state<File[]>([]);
	let tables = $state<Table[] | null>(null);
	let readError = $state('');
	let mode = $state<ImportMode>('merge');
	let checked = $state<{ findings: Finding[]; changes: ItemChange[] } | null>(null);
	let checking = $state(false);
	let asking = $state(false);
	let saving = $state(false);
	let saveError = $state('');
	let saved = $state('');
	let input: HTMLInputElement | undefined = $state();

	const base = $derived(`${ctx.version.configId}-v${ctx.version.version}`);
	const result = $derived<ImportResult | null>(tables ? importTables(ctx.content, tables, mode) : null);
	const totals = $derived(
		result
			? result.sections.reduce((t, s) => ({ added: t.added + s.added.length, changed: t.changed + s.changed.length, removed: t.removed + s.removed.length }), { added: 0, changed: 0, removed: 0 })
			: { added: 0, changed: 0, removed: 0 }
	);
	const changes = $derived(totals.added + totals.changed + totals.removed);
	const errors = $derived(checked?.findings.filter((f) => f.severity === 'error') ?? []);
	const fileNames = $derived(files.map((f) => f.name).join(', '));

	function download(name: string, data: BlobPart, type: string) {
		const url = URL.createObjectURL(new Blob([data], { type }));
		const a = document.createElement('a');
		a.href = url;
		a.download = name;
		a.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}
	const downloadWorkbook = () => download(`${base}.xlsx`, writeXlsx(toWorkbook(ctx.content)) as BlobPart, 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet');
	const downloadJson = () => download(`${base}.json`, JSON.stringify(ctx.content, null, 1), 'application/json');

	function bytesOf(f: File): Promise<Uint8Array> {
		if (typeof f.arrayBuffer === 'function') return f.arrayBuffer().then((b) => new Uint8Array(b));
		return new Promise((ok, fail) => {
			const r = new FileReader();
			r.onload = () => ok(new Uint8Array(r.result as ArrayBuffer));
			r.onerror = () => fail(r.error);
			r.readAsArrayBuffer(f);
		});
	}
	const textOf = async (f: File) => new TextDecoder().decode(await bytesOf(f));

	async function read(list: FileList | null) {
		files = list ? Array.from(list) : [];
		tables = null;
		checked = null;
		readError = '';
		saved = '';
		saveError = '';
		if (!files.length) return;
		try {
			const out: Table[] = [];
			for (const f of files) {
				const ext = f.name.toLowerCase().split('.').pop();
				if (ext === 'xlsx') out.push(...tablesFromSheets(await readXlsx(await bytesOf(f))));
				else if (ext === 'csv' || ext === 'txt') {
					const rows = readCsv(await textOf(f));
					out.push({ name: f.name.replace(/\.(csv|txt)$/i, ''), header: rows[0] ?? [], rows: rows.slice(1) });
				} else if (ext === 'json') {
					const j = tablesFromJson(await textOf(f));
					if (j.error) throw new Error(`${f.name}: ${j.error}`);
					out.push(...j.tables);
				} else throw new Error(`${f.name}: choose an Excel workbook (.xlsx), CSV files or a JSON file`);
			}
			tables = out;
		} catch (e) {
			readError = (e as Error).message;
		}
	}

	// the server checks the content the import would save, as the forms' checks do
	$effect(() => {
		const r = result;
		checked = null;
		if (!r || r.problems.length || !ctx.canEdit) return;
		checking = true;
		let live = true;
		ctx.api.check(ctx.version.configId, ctx.version.version, r.content).then(
			(res) => {
				if (live) checked = { findings: res.findings, changes: res.changes };
			},
			(e: ApiError) => {
				if (live) checked = { findings: [{ path: '', code: e.code, severity: 'error', message: e.message }], changes: [] };
			}
		).finally(() => live && (checking = false));
		return () => (live = false);
	});

	async function save(reason: string) {
		if (!result) return;
		saving = true;
		saveError = '';
		try {
			await ctx.save(result.content as Content, reason);
			saved = `Saved to draft v${ctx.version.version}: ${totals.added} added, ${totals.changed} changed, ${totals.removed} removed, in one change.`;
			asking = false;
			tables = null;
			files = [];
			if (input) input.value = '';
		} catch (e) {
			const err = e as ApiError;
			saveError = err.code === 'STALE_VERSION' ? 'Someone else saved this draft meanwhile. Choose the file again to import into the latest draft.' : `${err.code}: ${err.message}`;
			asking = false;
		} finally {
			saving = false;
		}
	}
</script>

<div class="stack">
	<section class="card" aria-label="Export">
		<h3>Download this version</h3>
		<p class="small muted">
			An Excel workbook with one sheet per section (accounts, events, posting rules and their lines, test cases …), a Contents sheet and a Columns sheet that says how each column is
			written. Fill it in or change it, then import it into a draft. An empty draft downloads the headers only: a template.
		</p>
		<div class="row">
			<button class="btn primary" onclick={downloadWorkbook}>Download workbook (.xlsx)</button>
			<button class="btn" onclick={downloadJson}>Download JSON</button>
		</div>
	</section>

	<section class="card" aria-label="Import">
		<h3>Import into this draft</h3>
		{#if !ctx.canEdit}
			<p class="notice small">
				Import works on a draft, by someone who edits configurations. {#if ctx.isEditor}<button class="btn" onclick={() => ctx.startDraft({ tab: 'transfer' })}
						>New draft from v{ctx.version.version}</button
					>{/if}
			</p>
		{:else}
			<p class="small muted">
				A workbook (.xlsx), CSV files (one per sheet, named like it: <span class="mono">accounts.csv</span>, <span class="mono">postingRules.lines.csv</span>) or a JSON file.
				Items are matched by their code. Lists are written <span class="mono">A | B</span>, pairs <span class="mono">NAME=value | NAME=value</span>, dates
				<span class="mono">yyyy-mm-dd</span>. Nothing is saved until you save it.
			</p>
			<div class="row">
				<label class="lbl">Files <input bind:this={input} class="field" type="file" accept=".xlsx,.csv,.txt,.json" multiple aria-label="Files to import" onchange={(e) => read((e.currentTarget as HTMLInputElement).files)} /></label>
				<fieldset class="mode">
					<legend class="lbl">What the file does</legend>
					<label><input type="radio" name="mode" value="merge" bind:group={mode} /> Add and update — keep everything the file does not have</label>
					<label><input type="radio" name="mode" value="replace" bind:group={mode} /> Replace — each section in the file holds exactly the file’s items</label>
				</fieldset>
			</div>
			{#if readError}<div class="notice bad" role="alert">{readError}</div>{/if}
			{#if saved}<div class="notice ok" role="status">{saved}</div>{/if}
			{#if saveError}<div class="notice bad" role="alert">{saveError}</div>{/if}

			{#if result}
				<div class="preview" aria-label="Import preview">
					<h4>What the import will do <span class="small muted">· {fileNames}</span></h4>
					{#if result.problems.length}
						<div class="notice bad" role="alert">
							<strong>{result.problems.length} problem{result.problems.length === 1 ? '' : 's'} in the file — nothing can be saved until they are fixed:</strong>
							<ul>{#each result.problems.slice(0, 40) as p (p)}<li>{p}</li>{/each}</ul>
							{#if result.problems.length > 40}<p>… and {result.problems.length - 40} more.</p>{/if}
						</div>
					{/if}
					{#if result.sections.length}
						<div class="tw">
							<table>
								<thead><tr><th>Section</th><th class="n">Added</th><th class="n">Changed</th><th class="n">Removed</th><th class="n">Unchanged</th><th>Items</th></tr></thead>
								<tbody>
									{#each result.sections as s (s.section)}
										<tr>
											<td>{s.title}</td>
											<td class="n" class:add={s.added.length}>{s.added.length}</td>
											<td class="n" class:chg={s.changed.length}>{s.changed.length}</td>
											<td class="n" class:del={s.removed.length}>{s.removed.length}</td>
											<td class="n muted">{s.unchanged}</td>
											<td class="mono small">
												{#if s.added.length}<div>+ {s.added.slice(0, 12).join(', ')}{s.added.length > 12 ? ` … (${s.added.length})` : ''}</div>{/if}
												{#if s.changed.length}<div>~ {s.changed.slice(0, 12).join(', ')}{s.changed.length > 12 ? ` … (${s.changed.length})` : ''}</div>{/if}
												{#if s.removed.length}<div>− {s.removed.slice(0, 12).join(', ')}{s.removed.length > 12 ? ` … (${s.removed.length})` : ''}</div>{/if}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
					{#if result.ignored.length}<p class="small muted">Not configuration, ignored: {result.ignored.join(', ')}.</p>{/if}
					{#if !result.problems.length}
						{#if checking}<p class="small muted">Checking…</p>{/if}
						{#if checked}
							{#if errors.length}
								<div class="notice warn">
									After the import the draft has {errors.length} error{errors.length === 1 ? '' : 's'}: it can be saved, but not validated until they are fixed.
								</div>
								<Findings findings={checked.findings.slice(0, 30)} />
							{:else}
								<p class="notice ok small">The draft checks out after the import{checked.findings.length ? ` (${checked.findings.length} warning${checked.findings.length === 1 ? '' : 's'})` : ''}.</p>
							{/if}
						{/if}
						<div class="row">
							<button class="btn primary" disabled={!changes || saving || checking} onclick={() => (asking = true)}>
								{changes ? `Save ${changes} change${changes === 1 ? '' : 's'} to draft` : 'Nothing to change'}
							</button>
						</div>
					{/if}
				</div>
			{/if}
		{/if}
	</section>
</div>

{#if asking && result}
	<ReasonDialog
		title={`Import into draft v${ctx.version.version}`}
		verb="Save to draft"
		detail={`${totals.added} added, ${totals.changed} changed, ${totals.removed} removed, in ${result.sections.length} section${result.sections.length === 1 ? '' : 's'}, from ${fileNames}. Saved as one change.`}
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = false)}
	/>
{/if}

<style>
	.stack {
		display: grid;
		gap: 14px;
	}
	.row {
		display: flex;
		gap: 12px;
		align-items: flex-end;
		flex-wrap: wrap;
		margin-top: 10px;
	}
	.mode {
		border: none;
		padding: 0;
		margin: 0;
		display: grid;
		gap: 4px;
		font-size: 14px;
	}
	.preview {
		margin-top: 14px;
		border-top: 1px solid var(--line, #dfe3ec);
		padding-top: 10px;
	}
	.preview h4 {
		margin: 0 0 8px;
	}
	.tw {
		overflow-x: auto;
	}
	table {
		border-collapse: collapse;
		width: 100%;
		font-size: 13px;
	}
	th,
	td {
		border-bottom: 1px solid var(--line, #dfe3ec);
		padding: 5px 8px;
		text-align: left;
		vertical-align: top;
	}
	.n {
		text-align: right;
		width: 70px;
	}
	.add {
		color: var(--ok, #1d7a4a);
		font-weight: 600;
	}
	.chg {
		color: var(--warn, #9a6700);
		font-weight: 600;
	}
	.del {
		color: var(--bad, #b4232c);
		font-weight: 600;
	}
	ul {
		margin: 6px 0 0;
		padding-left: 18px;
	}
</style>
