<script lang="ts">
	import { untrack } from 'svelte';
	// Step 1 — the client's chart of accounts (and its class list). Bring the files; each sheet's
	// columns are found from its headers (or by the assistant), shown in a preview, and imported
	// with one reason. Every account keeps its source row. Or copy from another configuration,
	// or add accounts by hand.
	import type { S1 } from './ctx';
	import type { Content } from '../../lib/studio';
	import type { ConfigSummary } from '../../lib/types';
	import type { Suggestion } from '../api';
	import { loadFile, sheetLabel, type LoadedSheet } from '../files';
	import { accounts, COA_ROLES, CLASS_ROLES, detectColumns, detectKind, importCOA, importClasses, roleNames, servedBy, type Account, type Kind } from './model';
	import { clone } from '../../lib/content';
	import ReasonDialog from '../../components/ReasonDialog.svelte';
	import AIPanel from '../AIPanel.svelte';

	let { ctx }: { ctx: S1 } = $props();

	interface Plan {
		kind: Kind;
		cols: Record<string, string>;
		system: string;
		include: boolean;
		from: 'headers' | 'assistant' | 'you';
	}
	let plans = $state<Record<string, Plan>>({});
	let error = $state('');
	let asking = $state<'' | 'files' | 'copy' | 'account'>('');
	let saving = $state(false);
	let done = $state('');
	let defaultFrom = $state(untrack(() => ctx.version.effectiveFrom));
	let q = $state('');
	let edit = $state<Account | null>(null);
	let editIsNew = $state(false);
	// copy from another configuration
	let configs = $state<ConfigSummary[]>([]);
	let copyFrom = $state('');
	let copyAccounts = $state<Account[] | null>(null);

	const sheets = $derived(ctx.files.flatMap((f) => f.sheets));
	$effect(() => {
		for (const s of sheets) {
			const k = sheetLabel(s);
			if (plans[k]) continue;
			const kind = detectKind(s);
			plans[k] = { kind, cols: detectColumns(s.header, kind === 'CLASSES' ? CLASS_ROLES : COA_ROLES), system: ctx.systems[0] ?? 'SOURCE', include: kind === 'COA' || kind === 'CLASSES', from: 'headers' };
		}
	});
	$effect(() => {
		ctx.api.configs().then((c) => (configs = c), () => {});
	});

	async function addFiles(list: FileList | null) {
		error = '';
		const out = [];
		for (const f of Array.from(list ?? [])) {
			try {
				out.push(await loadFile(f));
			} catch (e) {
				error = (e as Error).message;
			}
		}
		ctx.addFiles(out);
	}

	function setKind(s: LoadedSheet, kind: Kind) {
		const p = plans[sheetLabel(s)];
		p.kind = kind;
		p.cols = detectColumns(s.header, kind === 'CLASSES' ? CLASS_ROLES : COA_ROLES);
		p.include = kind === 'COA' || kind === 'CLASSES';
		p.from = 'you';
	}

	// what importing the chosen sheets would do, in order
	const preview = $derived.by(() => {
		let c: Content = ctx.content;
		const out: { s: LoadedSheet; added: string[]; changed: string[]; unchanged: number; skipped: { row: number; reason: string }[] }[] = [];
		for (const s of sheets) {
			const p = plans[sheetLabel(s)];
			if (!p?.include) continue;
			if (p.kind === 'COA' && p.cols.account && p.cols.name) {
				const r = importCOA(c, s, p.cols, defaultFrom, ctx.me.actor);
				c = r.content;
				out.push({ s, ...r });
			} else if (p.kind === 'CLASSES' && p.cols.code) {
				const r = importClasses(c, s, p.cols, p.system.trim().toUpperCase() || 'SOURCE', ctx.me.actor);
				c = r.content;
				out.push({ s, ...r });
			}
		}
		return { content: c, sheets: out };
	});

	const list = $derived(accounts(ctx.content));
	const shown = $derived(q.trim() ? list.filter((a) => `${a.code} ${a.name} ${a.type ?? ''}`.toLowerCase().includes(q.trim().toLowerCase())) : list);
	const fileName = (id?: string) => ((ctx.content.sourceFiles ?? []) as { id: string; name: string; sheet?: string }[]).find((f) => f.id === id);

	async function loadCopy() {
		copyAccounts = null;
		if (!copyFrom) return;
		const [id, n] = copyFrom.split('@');
		const v = await ctx.api.version(id, Number(n));
		copyAccounts = ((v.content ?? {}).accounts ?? []) as Account[];
	}
	let copyVersions = $state<{ id: string; n: number; status: string }[]>([]);
	$effect(() => {
		Promise.all(configs.map((c) => ctx.api.versions(c.configId).then((vs) => vs.map((v) => ({ id: c.configId, n: v.version, status: v.status }))))).then(
			(x) => (copyVersions = x.flat().filter((v) => !(v.id === ctx.version.configId && v.n === ctx.version.version))),
			() => {}
		);
	});

	async function save(reason: string) {
		saving = true;
		error = '';
		try {
			if (asking === 'files') {
				await ctx.save(preview.content, reason);
				done = preview.sheets.map((x) => `${sheetLabel(x.s)}: ${x.added.length} added, ${x.changed.length} changed, ${x.skipped.length} skipped`).join(' · ');
				for (const x of preview.sheets) plans[sheetLabel(x.s)].include = false;
			} else if (asking === 'copy' && copyAccounts) {
				const c = clone(ctx.content);
				const mine = accounts(c);
				let n = 0;
				for (const a of copyAccounts) {
					if (mine.some((x) => x.code === a.code)) continue;
					const { source: _s, class: _c, classes: _cs, ...rest } = a;
					mine.push({ ...rest, attributes: { ...(rest.attributes ?? {}), copiedFrom: copyFrom } } as Account);
					n++;
				}
				c.accounts = mine;
				await ctx.save(c, reason);
				done = `${n} accounts copied from ${copyFrom} (classes are mapped in steps 2 and 3).`;
				copyAccounts = null;
			} else if (asking === 'account' && edit) {
				const c = clone(ctx.content);
				const mine = accounts(c).filter((x) => x.code !== edit!.code);
				const a: Account = { ...edit, code: edit.code.trim(), name: edit.name.trim() };
				for (const k of ['type', 'normalBalance', 'activeTo'] as const) if (!a[k]) delete a[k];
				c.accounts = editIsNew ? [...accounts(c), a] : accounts(c).map((x) => (x.code === a.code ? a : x));
				if (editIsNew && mine.length !== accounts(ctx.content).length) throw new Error(`account ${a.code} already exists`);
				await ctx.save(c, reason);
				done = `Account ${a.code} saved.`;
				edit = null;
			}
			asking = '';
		} catch (e) {
			error = (e as Error).message;
			asking = '';
		} finally {
			saving = false;
		}
	}

	function aiAccept(s: Suggestion) {
		if (s.kind !== 'columns') return;
		const d = s.data;
		const sheet = sheets.find((x) => x.file === d.file && (!d.sheet || x.sheet === d.sheet)) ?? sheets.find((x) => x.file === d.file);
		if (!sheet) return `No loaded file is called ${d.file}.`;
		const p = plans[sheetLabel(sheet)];
		if (['COA', 'CLASSES', 'ASSIGNMENTS', 'OTHER'].includes(d.kind)) p.kind = d.kind;
		const cols: Record<string, string> = {};
		for (const [role, h] of Object.entries(d.columns ?? {})) if (sheet.header.includes(String(h))) cols[role] = String(h);
		p.cols = cols;
		p.include = p.kind === 'COA' || p.kind === 'CLASSES';
		p.from = 'assistant';
		return `Applied to ${sheetLabel(sheet)}: check the preview before importing.`;
	}
</script>

<div class="with-ai">
	<div class="stack">
		{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
		{#if done}<p class="notice ok" role="status">{done}</p>{/if}

		<section class="card" aria-label="Bring the client's files">
			<div class="row">
				<h2 class="sec grow">1. Bring the client’s files</h2>
				<span class="small muted">Excel or CSV: the chart of accounts, the class list, the assignment workbook</span>
			</div>
			<label class="drop" class:disabled={!ctx.canEdit}>
				<input type="file" multiple accept=".xlsx,.csv" aria-label="Client files" onchange={(e) => addFiles((e.currentTarget as HTMLInputElement).files)} disabled={!ctx.canEdit} />
				<b>Drop files here or click to choose</b><span class="small muted">Nothing is saved until you import it.</span>
			</label>
			{#each sheets as s (sheetLabel(s))}
				{@const p = plans[sheetLabel(s)]}
				{#if p}
					<div class="sheet">
						<div class="row">
							<input type="checkbox" bind:checked={p.include} disabled={p.kind === 'ASSIGNMENTS' || p.kind === 'OTHER'} aria-label={`Import ${sheetLabel(s)}`} />
							<b>{sheetLabel(s)}</b><span class="small muted">{s.rows.length} rows · {s.header.length} columns</span>
							<span class="grow"></span>
							<label class="small">It holds
								<select class="field" value={p.kind} onchange={(e) => setKind(s, (e.currentTarget as HTMLSelectElement).value as Kind)}>
									<option value="COA">Chart of accounts</option><option value="CLASSES">Accounting class list</option><option value="ASSIGNMENTS">Account assignment rules</option><option value="OTHER">Something else</option>
								</select></label
							>
							<span class="pill {p.from === 'assistant' ? 'ai' : ''}">columns {p.from === 'headers' ? 'found from the headers' : p.from === 'assistant' ? 'from the assistant' : 'set by you'}</span>
						</div>
						{#if p.kind === 'ASSIGNMENTS'}
							<p class="small muted">Assignment rules are read in step 3, where you say which columns are conditions.</p>
						{:else if p.kind === 'OTHER'}
							<p class="small muted">Not imported. The assistant can still read it.</p>
						{:else}
							<div class="cols">
								{#if p.kind === 'CLASSES'}
									<label class="lbl">Source system <input class="field mono" bind:value={p.system} /></label>
								{/if}
								{#each p.kind === 'COA' ? COA_ROLES : CLASS_ROLES as role (role)}
									<label class="lbl"
										>{roleNames[role]}{#if (p.kind === 'COA' && (role === 'account' || role === 'name')) || (p.kind === 'CLASSES' && role === 'code')} *{/if}
										<select class="field" value={p.cols[role] ?? ''} onchange={(e) => {
												const v = (e.currentTarget as HTMLSelectElement).value;
												p.cols = { ...p.cols, [role]: v };
												if (!v) delete p.cols[role];
												p.from = 'you';
											}}>
											<option value="">— none —</option>
											{#each s.header as h, hi (hi)}<option value={h}>{h}</option>{/each}
										</select></label
									>
								{/each}
								{#if p.kind === 'COA'}<label class="lbl">Active from, when the file has no date <input class="field" type="date" bind:value={defaultFrom} /></label>{/if}
							</div>
							<p class="small muted">Other columns are kept with each account as they are{p.kind === 'COA' ? ' (attributes)' : ''}, with the row they came from.</p>
						{/if}
					</div>
				{/if}
			{/each}
			{#if preview.sheets.length}
				<div class="tw prev">
					<table class="grid">
						<thead><tr><th>Sheet</th><th>New</th><th>Changed</th><th>Unchanged</th><th>Not imported</th></tr></thead>
						<tbody>
							{#each preview.sheets as x (sheetLabel(x.s))}
								<tr>
									<td>{sheetLabel(x.s)}</td><td><b class="ok">{x.added.length}</b></td><td><b class="warn">{x.changed.length}</b></td><td class="muted">{x.unchanged}</td>
									<td>{#if x.skipped.length}<details><summary class="bad">{x.skipped.length} rows</summary>{#each x.skipped as k, ki (ki)}<div class="small">row {k.row}: {k.reason}</div>{/each}</details>{:else}<span class="muted">0</span>{/if}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<div class="row"><button class="btn primary" disabled={!ctx.canEdit || saving} onclick={() => (asking = 'files')}>Import {preview.sheets.length} sheet{preview.sheets.length === 1 ? '' : 's'}…</button></div>
			{/if}
		</section>

		<section class="card" aria-label="Start from an existing configuration">
			<details>
				<summary><b>Or start from an existing configuration</b> <span class="small muted">copy its accounts; classes are mapped in steps 2 and 3</span></summary>
				<div class="row top">
					<select class="field" bind:value={copyFrom} onchange={loadCopy} aria-label="Configuration to copy from">
						<option value="">Choose a version…</option>
						{#each copyVersions as v (v.id + v.n)}<option value={`${v.id}@${v.n}`}>{v.id} v{v.n} · {v.status}</option>{/each}
					</select>
					{#if copyAccounts}<span class="small">{copyAccounts.filter((a) => !list.some((x) => x.code === a.code)).length} new accounts of {copyAccounts.length}</span>
						<button class="btn" disabled={!ctx.canEdit} onclick={() => (asking = 'copy')}>Copy accounts…</button>{/if}
				</div>
			</details>
		</section>

		<section class="card" aria-label="Chart of accounts">
			<div class="row">
				<h2 class="sec grow">The client’s chart of accounts <span class="pill">{list.length}</span></h2>
				<input class="field" placeholder="Search accounts" bind:value={q} aria-label="Search accounts" />
				<button class="btn" disabled={!ctx.canEdit} onclick={() => { edit = { code: '', name: '', activeFrom: ctx.version.effectiveFrom }; editIsNew = true; }}>+ Add account</button>
			</div>
			{#if !list.length}
				<p class="muted empty">No accounts yet: import the client’s chart, copy one, or add accounts by hand.</p>
			{:else}
				<div class="tw">
					<table class="grid">
						<thead><tr><th>Account</th><th>Name</th><th>Type</th><th>Balance</th><th>Classes</th><th>From</th></tr></thead>
						<tbody>
							{#each shown.slice(0, 400) as a, ai (ai)}
								<tr class="click" onclick={() => { edit = clone(a); editIsNew = false; }}>
									<td class="mono"><b>{a.code}</b></td><td>{a.name}</td><td>{a.type ?? '—'}</td><td>{a.normalBalance ?? '—'}</td>
									<td>{#each servedBy(ctx.content, a) as c (c)}<span class="pill info">{c}</span> {:else}<span class="small muted">set in step 3</span>{/each}</td>
									<td class="small muted">{#if a.source?.file}{fileName(a.source.file)?.name ?? a.source.file} › row {a.source.row}{:else}by hand{/if}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		{#if edit}
			<section class="card" aria-label="Account">
				<h2 class="sec">{editIsNew ? 'New account' : `Account ${edit.code}`}</h2>
				<div class="cols">
					<label class="lbl">Account number * <input class="field mono" bind:value={edit.code} disabled={!editIsNew} /></label>
					<label class="lbl">Name * <input class="field" bind:value={edit.name} /></label>
					<label class="lbl">Type <select class="field" bind:value={edit.type}><option value={undefined}>—</option>{#each ['ASSET', 'LIABILITY', 'EQUITY', 'INCOME', 'EXPENSE'] as t (t)}<option value={t}>{t}</option>{/each}</select></label>
					<label class="lbl">Normal balance <select class="field" bind:value={edit.normalBalance}><option value={undefined}>—</option><option value="D">Debit</option><option value="C">Credit</option></select></label>
					<label class="lbl">Active from * <input class="field" type="date" bind:value={edit.activeFrom} /></label>
					<label class="lbl">Active to <input class="field" type="date" bind:value={edit.activeTo} /></label>
				</div>
				{#if edit.attributes && Object.keys(edit.attributes).length}
					<details class="small"><summary>From the client’s file ({Object.keys(edit.attributes).length} more columns)</summary>{#each Object.entries(edit.attributes) as [k, v] (k)}<div><span class="muted">{k}:</span> {v}</div>{/each}</details>
				{/if}
				<div class="row top">
					<button class="btn primary" disabled={!ctx.canEdit || !edit.code.trim() || !edit.name.trim() || !edit.activeFrom} onclick={() => (asking = 'account')}>Save…</button>
					<button class="btn ghost" onclick={() => (edit = null)}>Close</button>
				</div>
			</section>
		{/if}
	</div>
	<AIPanel
		api={ctx.api}
		area="stage1"
		step="import"
		configId={ctx.version.configId}
		version={ctx.version.version}
		documents={ctx.files}
		canAct={ctx.canEdit}
		accept={aiAccept}
		examples={['Analyze these files. Identify the accounts, accounting classes, assignment conditions and missing information.', 'Which columns hold the account number, its name and its type?', 'Are there duplicate accounts or rows with missing fields?']}
	/>
</div>

{#if asking}
	<ReasonDialog
		title={asking === 'files' ? 'Import into the draft' : asking === 'copy' ? 'Copy accounts' : 'Save account'}
		verb={asking === 'files' ? 'Import' : 'Save'}
		detail={asking === 'files' ? preview.sheets.map((x) => `${sheetLabel(x.s)}: ${x.added.length} new, ${x.changed.length} changed`).join('; ') : ''}
		busy={saving}
		onsubmit={save}
		oncancel={() => (asking = '')}
	/>
{/if}

<style>
	.grow {
		flex: 1;
	}
	.drop {
		position: relative;
		display: grid;
		place-items: center;
		gap: 2px;
		padding: 22px;
		border: 2px dashed var(--line);
		border-radius: 12px;
		background: var(--bg);
		cursor: pointer;
		text-align: center;
		margin: 6px 0 12px;
	}
	.drop:hover {
		border-color: var(--accent);
	}
	.drop input {
		position: absolute;
		inset: 0;
		opacity: 0;
		cursor: pointer;
	}
	.drop.disabled {
		opacity: 0.55;
	}
	.sheet {
		border: 1px solid var(--line);
		border-radius: 10px;
		padding: 10px 12px;
		margin-bottom: 10px;
		display: grid;
		gap: 8px;
	}
	.cols {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
		gap: 8px 12px;
	}
	.prev {
		margin: 6px 0 10px;
	}
	.ok {
		color: var(--ok);
	}
	.warn {
		color: var(--warn);
	}
	.bad {
		color: var(--bad);
		cursor: pointer;
	}
	.top {
		margin-top: 10px;
	}
	.empty {
		padding: 10px 0;
	}
	summary {
		cursor: pointer;
	}
</style>
