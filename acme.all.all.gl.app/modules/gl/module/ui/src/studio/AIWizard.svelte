<script lang="ts">
	// Configure from a BRD: upload the document, then four AI steps (events, measures, formulas,
	// debit/credit lines with test cases), each proposed by the AI, checked by the engine, chosen
	// item by item by the person and saved to the draft only when they approve it. The AI never
	// saves, never enters an account number and never approves a version.
	import { untrack } from 'svelte';
	import type { Ctx } from '../lib/ctx';
	import type { ApiError } from '../lib/api';
	import type { Finding } from '../lib/types';
	import type { ItemChange } from '../lib/studio';
	import { aiSteps, applyItems, chosenItems, gapItems, sourceOf, toBase64, accountCodeRe, MAX_SOURCE_CHARS, type AIItem, type AIProposal, type AIStatus, type AIStepName, type BRDText, type GapAccount } from '../lib/ai';
	import { amount } from '../lib/format';
	import { icon } from '../components/icons';
	import Findings from '../components/Findings.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let { ctx }: { ctx: Ctx } = $props();

	interface StepState {
		proposal: AIProposal | null;
		running: boolean;
		error: string;
		notes: string;
		chosen: Set<string>;
		accounts: Record<string, GapAccount>;
		check: { findings: Finding[]; changes: ItemChange[] } | null;
		saved: string;
	}
	const blankStep = (): StepState => ({ proposal: null, running: false, error: '', notes: '', chosen: new Set(), accounts: {}, check: null, saved: '' });

	const storeKey = untrack(() => `gl_ai:${ctx.me.tenantId}:${ctx.version.configId}:${ctx.version.version}`);
	function restore(): { brd: (BRDText & { chars: number }) | null; typed?: string; done: AIStepName[]; skipped: AIStepName[] } {
		try {
			const raw = sessionStorage.getItem(storeKey);
			if (raw) return JSON.parse(raw);
		} catch {
			/* storage unavailable: start fresh */
		}
		return { brd: null, done: [], skipped: [] };
	}
	const initial = restore();
	let brd = $state<(BRDText & { chars: number }) | null>(initial.brd);
	let typed = $state(initial.typed ?? '');
	/** What the AI works from: the BRD, the typed requirements, or both. */
	const source = $derived(sourceOf(brd, typed));
	const tooLong = $derived((source?.text.length ?? 0) > MAX_SOURCE_CHARS);
	let done = $state<AIStepName[]>(initial.done ?? []);
	let skipped = $state<AIStepName[]>(initial.skipped ?? []);
	$effect(() => {
		const snapshot = JSON.stringify({ brd, typed, done, skipped });
		try {
			sessionStorage.setItem(storeKey, snapshot);
		} catch {
			/* storage unavailable: the BRD lasts for this page only */
		}
	});

	let status = $state<AIStatus | null>(null);
	let uploading = $state(false);
	let uploadError = $state('');
	let showText = $state(false);
	let steps = $state<Record<AIStepName, StepState>>({ events: blankStep(), measures: blankStep(), formulas: blankStep(), rules: blankStep() });
	let asking = $state<AIStepName | null>(null);
	let saving = $state(false);
	let saveError = $state('');

	$effect(() => {
		ctx.api.aiStatus().then(
			(s) => (status = s),
			(e) => (status = { available: false, reason: (e as ApiError).message })
		);
	});

	const isDraft = $derived(ctx.version.status === 'draft');
	const isEditor = $derived(ctx.me.roles.includes('CONFIG_EDITOR'));
	const settled = (k: AIStepName) => done.includes(k) || skipped.includes(k);
	const open = (k: AIStepName) => {
		const i = aiSteps.findIndex((s) => s.key === k);
		return !!source && aiSteps.slice(0, i).every((s) => settled(s.key));
	};
	const current = $derived(aiSteps.find((s) => !settled(s.key))?.key ?? null);
	let viewing = $state<AIStepName | null>(null);
	const shown = $derived(viewing ?? current);

	function readFile(f: File): Promise<ArrayBuffer> {
		return new Promise((resolve, reject) => {
			const r = new FileReader();
			r.onload = () => resolve(r.result as ArrayBuffer);
			r.onerror = () => reject(r.error);
			r.readAsArrayBuffer(f);
		});
	}

	async function upload(e: Event) {
		const input = e.currentTarget as HTMLInputElement;
		const f = input.files?.[0];
		if (!f) return;
		uploading = true;
		uploadError = '';
		try {
			const data = toBase64(await readFile(f));
			brd = await ctx.api.aiReadBRD(ctx.version.configId, ctx.version.version, f.name, data);
			done = [];
			skipped = [];
			steps = { events: blankStep(), measures: blankStep(), formulas: blankStep(), rules: blankStep() };
			viewing = null;
		} catch (err) {
			const a = err as ApiError;
			uploadError = a.message ?? String(err);
		} finally {
			uploading = false;
			input.value = '';
		}
	}

	async function runStep(k: AIStepName) {
		if (!source || tooLong) return;
		const s = steps[k];
		s.running = true;
		s.error = '';
		s.proposal = null;
		s.check = null;
		s.chosen = new Set();
		s.saved = '';
		try {
			const r = await ctx.api.aiStep(ctx.version.configId, ctx.version.version, k, source, s.notes.trim() || undefined);
			const accounts: Record<string, GapAccount> = {};
			for (const g of r.proposal.gaps) if (g.kind === 'ACCOUNT_NUMBER') accounts[g.code] = { number: '', name: g.name ?? '' };
			s.accounts = accounts;
			s.proposal = r.proposal;
		} catch (err) {
			const a = err as ApiError;
			s.error = `${a.code ?? 'ERROR'}: ${a.message ?? String(err)}`;
		} finally {
			s.running = false;
		}
	}

	const groupsOf = (p: AIProposal) => [...new Set(p.items.filter((it) => it.group !== '').map((it) => it.group))];
	const sharedOf = (p: AIProposal) => p.items.filter((it) => it.group === '');
	const toggle = (k: AIStepName, id: string) => {
		const c = new Set(steps[k].chosen);
		if (c.has(id)) c.delete(id);
		else c.add(id);
		steps[k].chosen = c;
	};
	const groupIds = (p: AIProposal, g: string) => p.items.filter((it) => it.group === g).map((it) => it.id);
	const allIn = (k: AIStepName, g: string) => {
		const p = steps[k].proposal;
		return !!p && groupIds(p, g).every((id) => steps[k].chosen.has(id));
	};
	const toggleGroup = (k: AIStepName, g: string) => {
		const p = steps[k].proposal;
		if (!p) return;
		const ids = groupIds(p, g);
		const c = new Set(steps[k].chosen);
		if (ids.every((id) => c.has(id))) ids.forEach((id) => c.delete(id));
		else ids.forEach((id) => c.add(id));
		steps[k].chosen = c;
	};
	const selectAll = (k: AIStepName, on: boolean) => {
		const p = steps[k].proposal;
		steps[k].chosen = new Set(on && p ? p.items.filter((it) => it.group !== '').map((it) => it.id) : []);
	};

	function candidate(k: AIStepName) {
		const s = steps[k];
		if (!s.proposal) return null;
		const items = chosenItems(s.proposal, s.chosen);
		if (!items.length) return null;
		return applyItems(ctx.content, [...items, ...gapItems(s.proposal.gaps, s.accounts, ctx.version.effectiveFrom, ctx.content)]);
	}

	// Live check of what would be saved: the same validation as saving.
	let timer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const k = shown;
		if (!k) return;
		const s = steps[k];
		s.chosen;
		JSON.stringify(s.accounts);
		const cand = candidate(k);
		clearTimeout(timer);
		if (!cand) {
			untrack(() => (steps[k].check = null));
			return;
		}
		timer = setTimeout(() => {
			ctx.api.check(ctx.version.configId, ctx.version.version, cand).then(
				(r) => (steps[k].check = { findings: r.findings, changes: r.changes }),
				() => (steps[k].check = null)
			);
		}, 350);
	});

	async function approve(k: AIStepName, reason: string) {
		const s = steps[k];
		const cand = candidate(k);
		if (!cand || !s.proposal) return;
		saving = true;
		saveError = '';
		try {
			await ctx.save(cand, reason, { kind: 'AI_STEP', step: k, provider: s.proposal.provider, model: s.proposal.model, brd: source?.name });
			const n = chosenItems(s.proposal, s.chosen).length;
			s.saved = `${n} item${n === 1 ? '' : 's'} saved to draft v${ctx.version.version}.`;
			done = [...done.filter((x) => x !== k), k];
			asking = null;
			viewing = null;
		} catch (err) {
			const a = err as ApiError;
			saveError = `${a.code ?? 'ERROR'}: ${a.message ?? String(err)}`;
			asking = null;
		} finally {
			saving = false;
		}
	}

	function skip(k: AIStepName) {
		skipped = [...skipped.filter((x) => x !== k), k];
		viewing = null;
	}

	const blocking = (fs: Finding[]) => fs.filter((f) => f.severity === 'error');
	const goTab = (tab: 'events' | 'accounts' | 'simulate') => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, tab });
	const stepLabel = (k: AIStepName) => aiSteps.find((s) => s.key === k)!;
</script>

{#snippet evidence(list: { quote: string; found: boolean }[] | undefined)}
	{#if list?.length}
		<ul class="quotes">
			{#each list as q, i (i)}
				<li class:missing={!q.found}>
					<span class="q">“{q.quote}”</span>
					{#if q.found}<span class="badge ok">in the requirements</span>{:else}<span class="badge warn">not found in the requirements: check it</span>{/if}
				</li>
			{/each}
		</ul>
	{/if}
{/snippet}

{#snippet itemRow(k: AIStepName, it: AIItem, selectable: boolean)}
	<div class="item" class:on={steps[k].chosen.has(it.id)}>
		{#if selectable}
			<label class="pick">
				<input type="checkbox" checked={steps[k].chosen.has(it.id)} onchange={() => toggle(k, it.id)} />
				<span class="ilabel">{it.label}</span>
			</label>
		{:else}
			<span class="ilabel">{it.label}</span>
		{/if}
		<div class="meta">
			<span class="chip mono">{it.section}</span>
			<span class="chip">{it.change === 'add' ? 'new' : 'updated'}</span>
			{#if it.confidence}<span class="chip">confidence {it.confidence}</span>{/if}
		</div>
		{#if it.why}<p class="why">{it.why}</p>{/if}
		{#if it.parts?.length}
			<ul class="parts">
				{#each it.parts as pt (pt.code)}
					<li>
						<span class="mono"><b>{pt.code}</b></span>
						{#if pt.label}<span>{pt.label}</span>{/if}
						{#if pt.note}<span class="muted small">· {pt.note}</span>{/if}
						{@render evidence(pt.evidence)}
					</li>
				{/each}
			</ul>
		{/if}
		{@render evidence(it.evidence)}
	</div>
{/snippet}

<div class="ai">
	{#if !isDraft}
		<div class="notice info">
			AI steps work on a draft. This version is <b>{ctx.version.status}</b>.
			{#if isEditor}<button class="btn" onclick={() => ctx.startDraft({ configId: ctx.version.configId, version: ctx.version.version, tab: 'ai' })}>Start a draft for AI steps</button>{/if}
		</div>
	{:else if !isEditor}
		<div class="notice warn">Configuring from a BRD needs the CONFIG_EDITOR role.</div>
	{/if}

	<section class="card status">
		<div class="row">
			<span class="aiic" aria-hidden="true">{@html icon('ai', 22)}</span>
			<div class="grow">
				<h3>AI assistant</h3>
				{#if !status}
					<p class="small muted">Checking whether AI is set up…</p>
				{:else if status.available}
					<p class="small muted">
						Ready: <b>{status.provider}</b> · model <span class="mono">{status.model}</span>. It proposes; you choose and approve every step; the engine checks
						everything; account numbers are always yours to enter.
					</p>
				{:else}
					<p class="small"><span class="badge off">Not set up</span> {status.reason}</p>
					<p class="small muted">
						An administrator sets <span class="mono">GL_AI_SETTINGS</span> on the server to a settings file with <span class="mono">AI_PROVIDER=openai</span> and
						<span class="mono">OPENAI_API_KEY</span> (the QA Workbench's format). Everything else in the studio works without AI.
					</p>
				{/if}
			</div>
		</div>
	</section>

	<!-- Step 0: the BRD -->
	<section class="card">
		<div class="row">
			<span class="sn" class:ok={!!source}>{source ? '✓' : '0'}</span>
			<h3 class="grow">The requirements: a BRD, your own words, or both</h3>
			{#if source}<span class="badge ok">{source.name} · {source.text.length.toLocaleString()} characters</span>{/if}
		</div>
		<p class="small muted">
			Upload a BRD (Word .docx, text .txt or Markdown .md, at most 1.5 MB), type the requirements in your own words, or both: the AI reads them together. Reading
			the file sends nothing to the AI. When you run a step, this text and the configuration (codes, names, rules) go to the AI provider — never policy or customer
			data. Do not upload or type personal data.
		</p>
		<div class="row">
			<label class="btn upload" class:primary={!brd}>
				{@html icon('upload', 16)}<span>{brd ? 'Upload another BRD' : 'Choose a BRD file'}</span>
				<input type="file" accept=".docx,.txt,.md,.markdown" onchange={upload} disabled={uploading || !isDraft || !isEditor} aria-label="BRD file" />
			</label>
			{#if uploading}<span class="small muted">Reading…</span>{/if}
			{#if brd}<button class="btn link" onclick={() => (showText = !showText)}>{showText ? 'Hide the text' : 'Show the text read'}</button>{/if}
		</div>
		{#if uploadError}<div class="notice bad" role="alert">{uploadError}</div>{/if}
		{#if brd && showText}<pre class="brdtext">{brd.text}</pre>{/if}
		<label class="lbl">Requirements in your own words (optional with a BRD, or instead of one)
			<textarea
				class="field typed"
				rows="5"
				bind:value={typed}
				disabled={!isDraft || !isEditor}
				placeholder="e.g. Endorsements: when a policy changes mid-term, the policy system sends an endorsement event identified by the policy, term and endorsement numbers, with the additional premium and its GST. The additional premium is written premium; the GST is owed to the tax office; the policyholder owes both."
			></textarea>
		</label>
		<p class="small muted">
			{typed.trim() ? `${typed.trim().length.toLocaleString()} characters typed. ` : ''}The AI quotes the BRD or your text for every proposal, and every quote is checked
			against them.
		</p>
		{#if tooLong}<div class="notice bad" role="alert">Together the BRD and your text exceed {MAX_SOURCE_CHARS.toLocaleString()} characters: shorten your text or split the BRD.</div>{/if}
		{#if brd && done.length === 0 && skipped.length === 0}
			<p class="small">Uploading another BRD starts the steps again; what you already approved stays in the draft.</p>
		{/if}
	</section>

	<!-- The steps -->
	<ol class="stepper" aria-label="AI steps">
		{#each aiSteps as s (s.key)}
			<li>
				<button
					class:current={shown === s.key}
					disabled={!open(s.key)}
					onclick={() => (viewing = s.key)}
					aria-current={shown === s.key ? 'step' : undefined}
				>
					<span class="sn" class:ok={done.includes(s.key)}>{done.includes(s.key) ? '✓' : s.n}</span>
					<span class="slabel"><b>{s.n}. {s.label}</b><small>{done.includes(s.key) ? 'approved and saved' : skipped.includes(s.key) ? 'skipped' : open(s.key) ? 'ready' : 'after the step before'}</small></span>
				</button>
			</li>
		{/each}
	</ol>

	{#if shown && open(shown)}
		{@const st = steps[shown]}
		{@const def = stepLabel(shown)}
		<section class="card stepcard" aria-label="Step {def.n}: {def.label}">
			<div class="row">
				<span class="sn big">{def.n}</span>
				<div class="grow">
					<h3>{def.label}</h3>
					<p class="small muted">The AI proposes {def.what}. Nothing is saved until you choose items and approve.</p>
				</div>
			</div>
			<label class="lbl">Guidance for the AI (optional)
				<textarea class="field" rows="2" bind:value={steps[shown].notes} placeholder="e.g. Only motor and home; treat stamp duty as owed to the state revenue office"></textarea>
			</label>
			<div class="row">
				<button class="btn primary" disabled={!status?.available || st.running || !isDraft || !isEditor || tooLong} onclick={() => runStep(shown!)}>
					{st.running ? 'The AI is drafting; the engine is checking…' : st.proposal ? 'Ask the AI again' : `Ask the AI for step ${def.n}`}
				</button>
				{#if shown === 'formulas' && !done.includes('formulas')}<button class="btn" onclick={() => skip('formulas')}>Skip: no calculated amounts</button>{/if}
				{#if st.saved}<span class="badge ok">{st.saved}</span>{/if}
			</div>
			{#if st.running}<p class="small muted">This can take up to a minute: the AI drafts, the engine checks, and the AI repairs what the engine finds.</p>{/if}
			{#if st.error}<div class="notice bad" role="alert">{st.error}</div>{/if}

			{#if st.proposal}
				{@const p = st.proposal}
				<div class="facts">
					<span class="chip">{p.items.length} item{p.items.length === 1 ? '' : 's'} proposed</span>
					<span class="chip">{p.attempts} attempt{p.attempts === 1 ? '' : 's'}</span>
					<span class="chip mono">{p.model}</span>
					<span class="chip">{(p.usage.promptTokens + p.usage.completionTokens).toLocaleString()} tokens</span>
				</div>
				{#if p.problems.length}
					<div class="notice bad" role="alert">
						<b>The engine still finds problems after {p.attempts} attempts.</b> Fix them after saving, or ask again with guidance:
						<ul>{#each p.problems as x, i (i)}<li>{x}</li>{/each}</ul>
					</div>
				{/if}
				{#if p.questions.length}
					<div class="notice info"><b>The AI asks</b> (answer in the guidance above and ask again):<ul>{#each p.questions as q, i (i)}<li>{q}</li>{/each}</ul></div>
				{/if}
				{#if p.notes.length}
					<div class="notice"><b>Checks applied</b><ul>{#each p.notes as x, i (i)}<li class="small">{x}</li>{/each}</ul></div>
				{/if}

				{#if p.balance.length}
					<div class="balance">
						<h4>LHS = RHS</h4>
						{#each p.balance as b (b.rule)}
							<div class="eq">
								<span class="mono rule">{b.rule}</span>
								<span class="side-D">Dr {b.lhs.join(' + ')}</span>
								<span class="eqs">=</span>
								<span class="side-C">Cr {b.rhs.join(' + ')}</span>
								{#if b.conditional.length}<span class="small muted">({b.conditional.join('; ')})</span>{/if}
							</div>
						{/each}
					</div>
				{/if}
				{#if p.cases.length}
					<h4>Synthetic test cases</h4>
					<div class="scroll">
						<table class="data">
							<thead><tr><th>Case</th><th>Amounts</th><th>Lines</th><th class="num">Debit</th><th class="num">Credit</th><th></th></tr></thead>
							<tbody>
								{#each p.cases as c (c.code)}
									<tr>
										<td class="small"><span class="mono"><b>{c.code}</b></span><div class="muted">{c.description}</div></td>
										<td class="small mono">{Object.entries(c.measures ?? {}).map(([k, v]) => `${k} ${v}`).join(' · ')}</td>
										<td class="small">
											{#each c.lines as l, i (i)}
												<div><span class="side-{l.side}">{l.side === 'D' ? 'Dr' : 'Cr'}</span> <span class="mono">{l.class}</span> {amount(l.amount)} {#if l.account}<span class="mono muted">→ {l.account}</span>{:else if l.problem}<span class="muted">· {l.problem}</span>{/if}</div>
											{/each}
											{#if c.problem}<div class="bad">{c.problem}</div>{/if}
										</td>
										<td class="num">{c.debit ? amount(c.debit) : '—'}</td>
										<td class="num">{c.credit ? amount(c.credit) : '—'}</td>
										<td>{#if c.balanced}<span class="badge ok">LHS = RHS</span>{:else}<span class="badge bad">not balanced</span>{/if}</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}

				{#if p.gaps.length}
					<div class="gaps">
						<h4>For you to supply</h4>
						{#each p.gaps as g (g.kind + g.code)}
							<div class="gap">
								<p class="small"><span class="badge warn">{g.kind === 'ACCOUNT_NUMBER' ? 'Account number' : g.kind === 'SOURCE_SYSTEM' ? 'Source system' : 'Dimension values'}</span> {g.message}</p>
								{#if g.kind === 'ACCOUNT_NUMBER' && steps[shown].accounts[g.code]}
									<div class="row">
										<label class="lbl">Account number for {g.code}<input class="field mono" bind:value={steps[shown].accounts[g.code].number} placeholder="e.g. 2200300" /></label>
										<label class="lbl grow">Account name<input class="field" bind:value={steps[shown].accounts[g.code].name} /></label>
									</div>
									{#if steps[shown].accounts[g.code].number && !accountCodeRe.test(steps[shown].accounts[g.code].number.trim())}<p class="small bad">Letters, digits, '.', '_' or '-', at most 40.</p>{/if}
								{/if}
							</div>
						{/each}
					</div>
				{/if}

				<div class="choose">
					<div class="row">
						<h4 class="grow">Choose what to save</h4>
						<button class="btn link" onclick={() => selectAll(shown!, true)}>Select all</button>
						<button class="btn link" onclick={() => selectAll(shown!, false)}>Select none</button>
					</div>
					{#each groupsOf(p) as g (g)}
						<div class="group">
							<label class="pick ghead">
								<input type="checkbox" checked={allIn(shown!, g)} onchange={() => toggleGroup(shown!, g)} aria-label="Choose all of {g}" />
								<span class="mono">{g}</span><span class="small muted">choose all of it, or item by item below</span>
							</label>
							{#each p.items.filter((it) => it.group === g) as it (it.id)}{@render itemRow(shown!, it, true)}{/each}
						</div>
					{:else}
						<p class="small muted">Nothing proposed.</p>
					{/each}
					{#if sharedOf(p).length}
						<details class="shared">
							<summary class="small">Also saved with your choice, when needed: {sharedOf(p).map((x) => x.key).join(', ')}</summary>
							{#each sharedOf(p) as it (it.id)}{@render itemRow(shown!, it, false)}{/each}
						</details>
					{/if}
				</div>

				{#if st.check}
					<div class="checkres">
						<h4>What saving your choice would do</h4>
						{#if st.check.changes.length}
							<ul class="small">{#each st.check.changes as c (c.section + c.key)}<li><span class="mono">{c.section} {c.key}</span>: {c.change.toLowerCase()}</li>{/each}</ul>
						{/if}
						{#if blocking(st.check.findings).length}
							<p class="small"><span class="badge warn">{blocking(st.check.findings).length} blocking</span> The draft can be saved, but not validated until they are resolved (account numbers, dimension values, or the next steps).</p>
						{/if}
						{#if st.check.findings.length}<Findings findings={st.check.findings} />{:else}<p class="notice ok small">The engine finds nothing to fix.</p>{/if}
					</div>
				{/if}

				<div class="row nav">
					<span class="small muted grow">{st.chosen.size ? `${st.chosen.size} chosen` : 'Choose at least one item to approve.'}</span>
					<button class="btn primary" disabled={!st.chosen.size || saving || !isDraft || !isEditor} onclick={() => (asking = shown)}>Approve and save to draft</button>
				</div>
				{#if saveError}<div class="notice bad" role="alert">{saveError}</div>{/if}
			{/if}
		</section>
	{:else if source && !current}
		<section class="card donecard">
			<h3>All four steps are settled</h3>
			<p>
				The approved configuration is in draft v{ctx.version.version} like any other: open <button class="btn link" onclick={() => goTab('events')}>Business events</button>,
				enter any account numbers still missing in <button class="btn link" onclick={() => goTab('accounts')}>Chart of accounts</button>, run the test cases in
				<button class="btn link" onclick={() => goTab('simulate')}>Simulation</button>, then validate the version and have someone who did not edit it approve it.
			</p>
		</section>
	{/if}
</div>

{#if asking}
	<ReasonDialog
		title="Approve step {stepLabel(asking).n}: {stepLabel(asking).label}"
		verb="Save"
		detail="Saves your chosen items to draft v{ctx.version.version}. The audit trail records that the AI proposed them from {source?.name ?? 'the requirements'} and that you approved them."
		busy={saving}
		onsubmit={(r) => approve(asking!, r)}
		oncancel={() => (asking = null)}
	/>
{/if}

<style>
	.ai {
		display: grid;
		gap: 16px;
	}
	.ai > .card + .card {
		margin-top: 0;
	}
	.status .aiic {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		border-radius: 10px;
		background: var(--gl-accent-soft);
		color: var(--gl-accent);
		flex: none;
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
	h3 {
		margin: 0;
	}
	.card p {
		margin: 6px 0;
	}
	.sn {
		display: inline-grid;
		place-items: center;
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 8px;
		background: var(--gl-off-soft);
		color: var(--gl-off);
		font-weight: 700;
		font-size: 13px;
	}
	.sn.ok {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	.sn.big {
		width: 30px;
		height: 30px;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
	}
	.upload {
		position: relative;
		overflow: hidden;
	}
	.upload input {
		position: absolute;
		inset: 0;
		opacity: 0;
		cursor: pointer;
	}
	.brdtext {
		max-height: 320px;
		overflow: auto;
		white-space: pre-wrap;
		font: 12px/1.5 var(--gl-mono);
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
		border-radius: 8px;
		padding: 12px;
	}
	.stepper {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 8px;
	}
	.stepper button {
		width: 100%;
		display: flex;
		gap: 10px;
		align-items: center;
		text-align: left;
		font: inherit;
		padding: 10px 12px;
		border: 1px solid var(--gl-border);
		border-radius: 10px;
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
	}
	.stepper button:disabled {
		opacity: 0.55;
		cursor: not-allowed;
	}
	.stepper button.current {
		border-color: var(--gl-accent);
		background: var(--gl-accent-soft);
	}
	.slabel {
		display: grid;
		min-width: 0;
	}
	.slabel b {
		font-size: 14px;
		font-weight: 600;
	}
	.slabel small {
		font-size: 12px;
		color: var(--gl-subtle);
	}
	.stepcard {
		display: grid;
		gap: 12px;
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.notice ul {
		margin: 6px 0 0;
		padding-left: 1.2em;
	}
	.balance {
		border: 1px solid var(--gl-border);
		border-radius: 10px;
		padding: 12px 14px;
		background: var(--gl-surface-2);
	}
	.balance h4,
	.stepcard h4 {
		margin: 4px 0 8px;
		font-size: 12px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-subtle);
	}
	.eq {
		display: flex;
		flex-wrap: wrap;
		gap: 6px 10px;
		align-items: baseline;
		font-family: var(--gl-mono);
		font-size: 13px;
	}
	.eq .rule {
		font-weight: 700;
	}
	.eqs {
		color: var(--gl-subtle);
	}
	.gaps {
		display: grid;
		gap: 8px;
	}
	.gap {
		border: 1px dashed var(--gl-warn-line);
		border-radius: 10px;
		padding: 10px 12px;
	}
	.choose {
		display: grid;
		gap: 10px;
	}
	.group {
		display: grid;
		gap: 8px;
		border: 1px solid var(--gl-border);
		border-radius: 12px;
		padding: 12px;
	}
	.ghead {
		font-weight: 700;
		color: var(--gl-accent);
		align-items: center;
	}
	.item {
		display: grid;
		gap: 6px;
		padding: 10px 12px;
		border-radius: 10px;
		background: var(--gl-surface-2);
		border: 1px solid transparent;
	}
	.item.on {
		border-color: var(--gl-accent);
		background: var(--gl-accent-soft);
	}
	.pick {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		cursor: pointer;
	}
	.pick input {
		margin-top: 3px;
	}
	.ilabel {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.why {
		margin: 0;
		font-size: 13px;
		color: var(--gl-muted);
	}
	.parts {
		margin: 0;
		padding-left: 1.2em;
		display: grid;
		gap: 4px;
		font-size: 13px;
	}
	.quotes {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 4px;
	}
	.quotes li {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: baseline;
		font-size: 13px;
	}
	.quotes .q {
		font-style: italic;
		color: var(--gl-muted);
	}
	.shared summary {
		cursor: pointer;
		color: var(--gl-muted);
	}
	.checkres {
		border-top: 1px solid var(--gl-border);
		padding-top: 12px;
	}
	.bad {
		color: var(--gl-bad);
	}
	.nav {
		border-top: 1px solid var(--gl-border);
		padding-top: 12px;
	}
	.donecard p {
		margin: 6px 0 0;
	}
</style>
