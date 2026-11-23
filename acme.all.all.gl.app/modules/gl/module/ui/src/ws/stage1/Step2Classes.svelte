<script lang="ts">
	import { untrack } from 'svelte';
	// Step 2 — the source system's accounting class codes, kept as received, mapped to the
	// ecosystem's standard classes (or, only when none has the meaning, a custom class). A source
	// class that means several things gets one mapping per meaning, each with its condition.
	// Changes collect here and are saved together with one reason.
	import type { S1 } from './ctx';
	import type { Content } from '../../lib/studio';
	import { clone } from '../../lib/content';
	import type { StandardClass, Suggestion } from '../api';
	import { adopt, classes, rules, setMappings, sourceClasses, NATURES, type Mapping, type SourceClass } from './model';
	import ReasonDialog from '../../components/ReasonDialog.svelte';
	import AIPanel from '../AIPanel.svelte';

	let { ctx }: { ctx: S1 } = $props();

	let work = $state<Content>(untrack(() => clone(ctx.content)));
	let changes = $state<string[]>([]);
	let selected = $state('');
	let q = $state('');
	let asking = $state(false);
	let saving = $state(false);
	let error = $state('');
	let done = $state('');
	let custom = $state({ code: '', name: '', nature: 'ASSET', normalSide: 'D', description: '' });
	let showCustom = $state(false);
	let manual = $state({ system: untrack(() => ctx.systems[0] ?? ''), code: '', meaning: '' });

	const lib = $derived(ctx.library);
	const approved = $derived(lib.classes.filter((c) => c.status === 'APPROVED').length);
	const srcs = $derived(sourceClasses(work));
	const mine = $derived(classes(work));
	const sel = $derived(srcs.find((s) => `${s.system}/${s.code}` === selected) ?? null);
	const libShown = $derived(q.trim() ? lib.classes.filter((c) => `${c.code} ${c.name} ${c.definition}`.toLowerCase().includes(q.trim().toLowerCase())) : lib.classes);
	const groups = $derived([...new Set(libShown.map((c) => c.group))]);
	const std = (code: string) => lib.classes.find((c) => c.code === code);
	const usedBy = (code: string) => srcs.filter((s) => (s.mappings ?? []).some((m) => m.class === code)).length;
	const ruleCount = (code: string) => rules(work).filter((r) => r.class === code).length;
	const mapped = $derived(srcs.filter((s) => s.status === 'NOT_USED' || (s.mappings ?? []).length).length);

	function note(what: string) {
		changes = [...changes, what];
		done = '';
	}

	function mapTo(s: SourceClass, code: string, when?: Record<string, string>) {
		const c = clone(work);
		const target = std(code);
		if (target) adopt(c, target);
		const ms: Mapping[] = [...(s.mappings ?? []).filter((m) => !(m.class === code && JSON.stringify(m.when ?? {}) === JSON.stringify(when ?? {}))), { class: code, ...(when && Object.keys(when).length ? { when } : {}) }];
		setMappings(c, s.system, s.code, ms);
		work = c;
		note(`${s.code} → ${code}${when && Object.keys(when).length ? ` when ${Object.entries(when).map(([k, v]) => `${k} = ${v}`).join(', ')}` : ''}`);
	}
	// a standard class used without a source class (e.g. claims, whose system sends no class codes)
	function use(code: string) {
		const target = std(code);
		if (!target || mine.some((x) => x.code === code)) return;
		work = adopt(clone(work), target);
		note(`${code} used for this client`);
	}
	function unmap(s: SourceClass, i: number) {
		const c = clone(work);
		const ms = (s.mappings ?? []).filter((_, j) => j !== i);
		setMappings(c, s.system, s.code, ms);
		work = c;
		note(`${s.code}: mapping removed`);
	}
	function notUsed(s: SourceClass, on: boolean) {
		const c = clone(work);
		setMappings(c, s.system, s.code, on ? [] : (s.mappings ?? []), on ? 'NOT_USED' : undefined);
		work = c;
		note(`${s.code}: ${on ? 'not used by GL' : 'used again'}`);
	}
	function addCustom() {
		const c = clone(work);
		const x = { code: custom.code.trim().toUpperCase(), name: custom.name.trim(), nature: custom.nature, normalSide: custom.normalSide, description: custom.description.trim() || undefined, custom: true };
		if (classes(c).some((k) => k.code === x.code) || std(x.code)) {
			error = `${x.code} already exists${std(x.code) ? ' in the standard library: map to it instead' : ''}.`;
			return;
		}
		c.accountingClasses = [...classes(c), x];
		work = c;
		note(`custom class ${x.code}`);
		custom = { code: '', name: '', nature: 'ASSET', normalSide: 'D', description: '' };
		showCustom = false;
	}
	function addManual() {
		const c = clone(work);
		if (sourceClasses(c).some((s) => s.system === manual.system.trim().toUpperCase() && s.code === manual.code.trim())) {
			error = `${manual.code} is already listed.`;
			return;
		}
		c.sourceClasses = [...sourceClasses(c), { system: manual.system.trim().toUpperCase(), code: manual.code.trim(), meaning: manual.meaning.trim() || undefined }];
		work = c;
		note(`source class ${manual.code} added`);
		selected = `${manual.system.trim().toUpperCase()}/${manual.code.trim()}`;
		manual = { ...manual, code: '', meaning: '' };
	}

	// the condition being added to the selected source class
	let cond = $state({ target: '', key: '', value: '' });

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
		if (s.kind === 'mapping') {
			let src = srcs.find((x) => x.code === d.code && (!d.system || x.system === d.system));
			if (!src) {
				const c = clone(work);
				c.sourceClasses = [...sourceClasses(c), { system: String(d.system || ctx.systems[0] || 'SOURCE').toUpperCase(), code: String(d.code) }];
				work = c;
				src = sourceClasses(work).at(-1)!;
			}
			for (const m of (d.mappings ?? []) as { standard: string; when?: Record<string, string> }[]) if (std(m.standard)) mapTo(src, m.standard, m.when);
			selected = `${src.system}/${src.code}`;
			return 'Added to your changes: save them when you are done.';
		}
		if (s.kind === 'newClass') {
			custom = { code: String(d.code ?? ''), name: String(d.name ?? ''), nature: NATURES.includes(d.nature) ? d.nature : 'ASSET', normalSide: d.normalSide === 'C' ? 'C' : 'D', description: String(d.definition ?? '') };
			showCustom = true;
			return 'Opened the custom class form: check it, then add it.';
		}
	}
</script>

<div class="with-ai">
	<div class="stack">
		{#if approved < lib.classes.length}
			<p class="notice warn small">
				<b>The standard class library v{lib.version} is {lib.status.toLowerCase()}</b> ({approved} of {lib.classes.length} classes approved). You can draft mappings now; the configuration
				cannot be submitted against classes the product owner has not approved.
			</p>
		{/if}
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

		<section class="card" aria-label="Source classes">
			<div class="row">
				<h2 class="sec grow">Source accounting classes <span class="pill">{mapped} of {srcs.length} mapped</span></h2>
			</div>
			{#if !srcs.length}
				<p class="muted">No source classes yet: import the client’s class list in step 1, or add them here.</p>
			{:else}
				<div class="tw">
					<table class="grid">
						<thead><tr><th>Source</th><th>Code</th><th>Meaning</th><th>Mapped to</th><th>Status</th></tr></thead>
						<tbody>
							{#each srcs as s, si (si)}
								<tr class="click" class:sel={selected === `${s.system}/${s.code}`} onclick={() => (selected = `${s.system}/${s.code}`)}>
									<td class="small muted">{s.system}</td>
									<td class="mono"><b>{s.code}</b></td>
									<td class="small">{s.meaning ?? ''}</td>
									<td>{#each s.mappings ?? [] as m, i (i)}<span class="pill info">{m.class}{#if m.when && Object.keys(m.when).length}<small> when {Object.entries(m.when).map(([k, v]) => `${k}=${v}`).join(', ')}</small>{/if}</span> {/each}</td>
									<td>{#if s.status === 'NOT_USED'}<span class="pill">not used</span>{:else if (s.mappings ?? []).length}<span class="pill ok">mapped</span>{:else}<span class="pill warn">to map</span>{/if}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
			<details class="manual">
				<summary class="small">Add a source class by hand</summary>
				<div class="row">
					<input class="field mono" bind:value={manual.system} placeholder="SYSTEM" aria-label="Source system" style="width:120px" />
					<input class="field mono" bind:value={manual.code} placeholder="Code, as in the source" aria-label="Source class code" />
					<input class="field grow" bind:value={manual.meaning} placeholder="Meaning" aria-label="Meaning" />
					<button class="btn" disabled={!ctx.canEdit || !manual.system.trim() || !manual.code.trim()} onclick={addManual}>Add</button>
				</div>
			</details>
		</section>

		{#if sel}
			<section class="card selcard" aria-label="Mapping">
				<div class="row">
					<h2 class="sec grow"><span class="mono">{sel.code}</span> <span class="muted small">({sel.system})</span> — {sel.meaning ?? 'no meaning given'}</h2>
					<label class="small"><input type="checkbox" checked={sel.status === 'NOT_USED'} disabled={!ctx.canEdit} onchange={(e) => notUsed(sel!, (e.currentTarget as HTMLInputElement).checked)} /> Not used by GL</label>
				</div>
				{#if sel.role}<p class="small muted">Role in the source: {sel.role}</p>{/if}
				{#if sel.note}<p class="small muted">Source notes: {sel.note}</p>{/if}
				{#if sel.source?.file}<p class="small muted">From {sel.source.file} row {sel.source.row}; the code is kept exactly as received.</p>{/if}
				{#each sel.mappings ?? [] as m, i (i)}
					<div class="map row">
						<span class="pill info">{m.class}</span>
						<span class="small">{std(m.class)?.name ?? mine.find((x) => x.code === m.class)?.name ?? ''}</span>
						{#if m.when && Object.keys(m.when).length}<span class="small muted">only when {Object.entries(m.when).map(([k, v]) => `${k} = ${v}`).join(' and ')}</span>{/if}
						{#if std(m.class)?.status === 'PROPOSED'}<span class="pill warn">library class not approved yet</span>{/if}
						<span class="grow"></span>
						<button class="btn ghost small" disabled={!ctx.canEdit} onclick={() => unmap(sel!, i)}>Remove</button>
					</div>
				{/each}
				<div class="row addmap">
					<select class="field" bind:value={cond.target} aria-label="Standard class">
						<option value="">Map to…</option>
						<optgroup label="Standard library">{#each lib.classes as c (c.code)}<option value={c.code}>{c.code} · {c.name}</option>{/each}</optgroup>
						{#if mine.some((x) => x.custom)}<optgroup label="Custom classes of this client">{#each mine.filter((x) => x.custom) as c (c.code)}<option value={c.code}>{c.code} · {c.name}</option>{/each}</optgroup>{/if}
					</select>
					<span class="small muted">only when</span>
					<input class="field mono" bind:value={cond.key} placeholder="CONDITION (optional)" aria-label="Condition" style="width:200px" oninput={() => (cond.key = cond.key.toUpperCase())} />
					<span class="small muted">=</span>
					<input class="field mono" bind:value={cond.value} placeholder="value" aria-label="Condition value" style="width:120px" />
					<button class="btn primary small" disabled={!ctx.canEdit || !cond.target || (!!cond.key && !cond.value)} onclick={() => { mapTo(sel!, cond.target, cond.key ? { [cond.key]: cond.value } : undefined); cond = { target: '', key: '', value: '' }; }}>Add mapping</button>
				</div>
				<p class="small muted">A source class with one meaning maps once. When it means several things (for example premium and tax), add one mapping per meaning, each with the condition that tells them apart.</p>
			</section>
		{/if}

		<section class="card" aria-label="Classes of this client">
			<div class="row">
				<h2 class="sec grow">Classes this client uses <span class="pill">{mine.length}</span></h2>
				<button class="btn" disabled={!ctx.canEdit} onclick={() => (showCustom = !showCustom)}>+ Custom class</button>
			</div>
			{#if showCustom}
				<div class="customform">
					<p class="small muted">Only when no standard class has the meaning. A custom class stays with this client.</p>
					<div class="row">
						<input class="field mono" bind:value={custom.code} placeholder="CODE" aria-label="Custom class code" oninput={() => (custom.code = custom.code.toUpperCase())} />
						<input class="field grow" bind:value={custom.name} placeholder="Name" aria-label="Custom class name" />
						<select class="field" bind:value={custom.nature} aria-label="Nature">{#each NATURES as n (n)}<option value={n}>{n}</option>{/each}</select>
						<select class="field" bind:value={custom.normalSide} aria-label="Normal side"><option value="D">Debit</option><option value="C">Credit</option></select>
					</div>
					<input class="field wide" bind:value={custom.description} placeholder="Business definition" aria-label="Definition" />
					<div class="row"><button class="btn primary small" disabled={!/^[A-Z][A-Z0-9_]*$/.test(custom.code) || !custom.name.trim()} onclick={addCustom}>Add class</button></div>
				</div>
			{/if}
			{#if mine.length}
				<div class="tw">
					<table class="grid">
						<thead><tr><th>Class</th><th>Name</th><th>Nature</th><th>Origin</th><th>Source classes</th><th>Rules</th></tr></thead>
						<tbody>
							{#each mine as c, ci (ci)}
								<tr>
									<td class="mono"><b>{c.code}</b></td><td>{c.name}</td><td class="small">{c.nature}</td>
									<td>{#if c.standard}<span class="pill {std(c.standard)?.status === 'APPROVED' ? 'ok' : 'warn'}">standard · {std(c.standard)?.status.toLowerCase() ?? 'unknown'}</span>{:else if c.custom}<span class="pill ai">custom</span>{:else}<span class="pill">existing</span>{/if}</td>
									<td>{usedBy(c.code)}</td><td>{ruleCount(c.code)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}<p class="muted small">None yet: mapping a source class adopts its standard class.</p>{/if}
		</section>

		<section class="card" aria-label="Standard class library">
			<details>
				<summary><b>Standard class library</b> <span class="small muted">v{lib.version} · {lib.classes.length} classes · {lib.status.toLowerCase()}</span></summary>
				<input class="field search" placeholder="Search the library" bind:value={q} aria-label="Search the library" />
				{#each groups as g (g)}
					<h3 class="grp">{g}</h3>
					{#each libShown.filter((c) => c.group === g) as c (c.code)}
						<div class="libc row">
							<div class="grow"><span class="mono"><b>{c.code}</b></span> {c.name} <span class="small muted">· {c.nature} · {c.normalSide === 'D' ? 'debit' : 'credit'}</span><br /><span class="small">{c.definition}</span></div>
							{#if mine.some((x) => x.code === c.code)}<span class="pill ok">used</span>{:else}<button class="btn small" disabled={!ctx.canEdit} onclick={() => use(c.code)}>Use for this client</button>{/if}
						</div>
					{/each}
				{/each}
			</details>
		</section>
	</div>
	<AIPanel
		api={ctx.api}
		area="stage1"
		step="classes"
		configId={ctx.version.configId}
		version={ctx.version.version}
		documents={ctx.files}
		selection={sel ? { sourceClass: sel.code, system: sel.system } : undefined}
		canAct={ctx.canEdit}
		accept={aiAccept}
		examples={['Map all source accounting classes to the standard library. Reuse existing classes where possible and show exceptions.', 'Which source classes mean more than one thing, and what tells them apart?', 'Are any standard classes duplicated or unnecessary for this client?']}
	/>
</div>

{#if asking}
	<ReasonDialog title="Save class mappings" verb="Save" detail={changes.join('; ')} busy={saving} onsubmit={save} oncancel={() => (asking = false)} />
{/if}

<style>
	.grow {
		flex: 1;
	}
	.pending {
		border-color: var(--warn);
		background: var(--warn-soft);
	}
	.selcard {
		border-color: var(--accent);
	}
	.map {
		padding: 6px 0;
		border-bottom: 1px solid var(--line);
	}
	.addmap {
		margin: 10px 0 6px;
	}
	.manual {
		margin-top: 10px;
	}
	.manual summary,
	details summary {
		cursor: pointer;
	}
	.customform {
		display: grid;
		gap: 8px;
		margin-bottom: 12px;
		padding: 10px;
		border: 1px dashed var(--line);
		border-radius: 10px;
	}
	.wide {
		width: 100%;
		box-sizing: border-box;
	}
	.search {
		margin: 10px 0;
		width: 100%;
		box-sizing: border-box;
	}
	.grp {
		font-size: 12px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--faint);
		margin: 12px 0 6px;
	}
	.libc {
		padding: 6px 0;
		border-bottom: 1px solid var(--line);
	}
	.pill small {
		font-weight: 500;
		margin-left: 4px;
	}
</style>
