<script lang="ts">
	// The AI assistant of a page. It knows where it is (area, step, configuration version,
	// selection); the person asks, optionally with documents; the answer comes with suggestions,
	// each with its evidence and confidence. A suggestion is applied only when the person accepts
	// it, by the page, through the normal checks; every accept and reject is recorded.
	import type { Area, AssistantStatus, AssistResult, Suggestion, WsClient } from './api';
	import { asDocuments, loadFile, type LoadedFile } from './files';

	let {
		api,
		area,
		step = '',
		configId = '',
		version = 0,
		selection = undefined,
		documents = [],
		examples = [],
		canAct = false,
		accept = undefined
	}: {
		api: WsClient;
		area: Area;
		step?: string;
		configId?: string;
		version?: number;
		selection?: unknown;
		documents?: LoadedFile[];
		examples?: string[];
		canAct?: boolean;
		accept?: (s: Suggestion) => Promise<string | void> | string | void;
	} = $props();

	let status = $state<AssistantStatus | null>(null);
	let question = $state('');
	let extra = $state<LoadedFile[]>([]);
	let busy = $state(false);
	let error = $state('');
	let result = $state<AssistResult | null>(null);
	let decided = $state<Record<string, string>>({});

	$effect(() => {
		api.assistantStatus().then(
			(s) => (status = s),
			() => (status = { available: false, reason: 'The assistant could not be reached.' })
		);
	});

	const docs = $derived([...documents, ...extra]);
	const off = $derived(!status?.available);

	async function attach(list: FileList | null) {
		error = '';
		for (const f of Array.from(list ?? [])) {
			try {
				extra = [...extra, await loadFile(f)];
			} catch (e) {
				error = (e as Error).message;
			}
		}
	}

	async function ask() {
		if (!question.trim() || off) return;
		busy = true;
		error = '';
		result = null;
		decided = {};
		try {
			const r = await api.assist({ area, step: area === 'stage1' ? step : undefined, configId: configId || undefined, version: version || undefined, question, documents: asDocuments(docs), selection });
			result = r.result;
			if (status) status = { ...status, budget: r.budget };
		} catch (e) {
			const x = e as { code?: string; message: string };
			error = `${x.code ? x.code + ': ' : ''}${x.message}`;
		} finally {
			busy = false;
		}
	}

	async function record(s: Suggestion, decision: 'ACCEPTED' | 'EDITED' | 'REJECTED', detail: unknown) {
		try {
			await api.decide({ area, step: area === 'stage1' ? step : undefined, configId: configId || undefined, version: version || undefined, suggestionId: s.id, title: s.title, decision, detail, provider: result?.provider, model: result?.model });
		} catch {
			/* the decision still applies on the page; the record is retried with the next one */
		}
	}

	async function onAccept(s: Suggestion) {
		const note = await accept?.(s);
		decided = { ...decided, [s.id]: typeof note === 'string' && note ? note : 'Accepted' };
		await record(s, 'ACCEPTED', { kind: s.kind, data: s.data });
	}
	async function onReject(s: Suggestion) {
		decided = { ...decided, [s.id]: 'Rejected' };
		await record(s, 'REJECTED', { kind: s.kind });
	}

	const kindLabel: Record<string, string> = {
		roles: 'Roles',
		conflict: 'Access conflict',
		client: 'Client details',
		missing: 'Missing',
		columns: 'Columns',
		mapping: 'Class mapping',
		newClass: 'New class',
		rule: 'Assignment rule',
		conditions: 'Conditions',
		issue: 'Issue',
		note: 'Note'
	};
	const paras = (t: string) => t.split(/\n+/).map((x) => x.trim()).filter(Boolean);
</script>

<aside class="ai card" aria-label="AI assistant">
	<div class="head">
		<span class="spark">✦</span>
		<b>AI assistant</b>
		<span class="grow"></span>
		{#if status?.available}<span class="pill ai" title={`${status.provider} · ${status.model}`}>On</span>{:else if status}<span class="pill">Off</span>{/if}
	</div>
	{#if status && !status.available}
		<p class="notice small">{status.reason} <b>Manual mode works on every step.</b></p>
	{:else if status?.budget}
		<p class="small muted">Today: {status.budget.usedToday} of {status.budget.dailyRequests} requests for this client. It suggests; you decide.</p>
	{/if}
	<textarea class="field q" rows="3" bind:value={question} placeholder="Ask in plain words…" aria-label="Question for the AI assistant" disabled={off}></textarea>
	{#if examples.length}
		<div class="examples">
			{#each examples as x (x)}<button class="ex" onclick={() => (question = x)} disabled={off}>{x}</button>{/each}
		</div>
	{/if}
	<div class="row">
		<label class="btn small ghost attach" class:disabled={off}>📎 Attach<input type="file" multiple accept=".xlsx,.csv,.txt,.md" onchange={(e) => attach((e.currentTarget as HTMLInputElement).files)} disabled={off} /></label>
		<span class="grow"></span>
		<button class="btn ai" onclick={ask} disabled={off || busy || !question.trim()}>{busy ? 'Thinking…' : 'Ask AI'}</button>
	</div>
	{#if docs.length}<p class="small muted docs">Sent with the question: {docs.map((d) => d.name).join(', ')}</p>{/if}
	{#if error}<p class="notice bad small" role="alert">{error}</p>{/if}

	{#if result}
		<div class="answer">
			{#each paras(result.answer) as p, i (i)}{#if p.startsWith('- ') || p.startsWith('• ')}<p class="bullet">{p.slice(2)}</p>{:else}<p>{p}</p>{/if}{/each}
		</div>
		{#if result.truncated}<p class="notice warn small">Part of the documents was over the size limit and was not read.</p>{/if}
		{#each result.suggestions as s (s.id)}
			<div class="sg" class:done={decided[s.id]}>
				<div class="row">
					<span class="pill ai">{kindLabel[s.kind] ?? s.kind}</span>
					<span class="pill {s.confidence === 'high' ? 'ok' : s.confidence === 'medium' ? 'info' : 'warn'}">{s.confidence} confidence</span>
				</div>
				<b class="t">{s.title}</b>
				{#if s.why}<p class="small">{s.why}</p>{/if}
				{#if s.evidence.length}
					<ul class="ev">
						{#each s.evidence as e, i (i)}<li class:nf={!e.found}><span>{e.found ? '✓' : '✗'}</span> “{e.quote}”{#if !e.found}<em> — not found in the documents</em>{/if}</li>{/each}
					</ul>
				{/if}
				{#each s.notes ?? [] as n, ni (ni)}<p class="small warnline">⚠ {n}</p>{/each}
				{#if decided[s.id]}
					<p class="small muted">{decided[s.id]}</p>
				{:else if s.kind !== 'note' && s.kind !== 'issue' && s.kind !== 'missing'}
					<div class="row">
						<button class="btn small primary" onclick={() => onAccept(s)} disabled={!canAct || !accept}>Accept</button>
						<button class="btn small" onclick={() => onReject(s)}>Reject</button>
					</div>
				{/if}
			</div>
		{/each}
		{#if result.questions.length}
			<div class="qs"><b class="small">The assistant asks</b>{#each result.questions as q, qi (qi)}<p class="small">• {q}</p>{/each}</div>
		{/if}
		<p class="small muted">{result.provider} · {result.model} · {result.usage.promptTokens + result.usage.completionTokens} tokens{result.aliased ? ` · ${result.aliased} e-mail address(es) sent as aliases` : ''}</p>
	{/if}
</aside>

<style>
	.ai {
		position: sticky;
		top: 74px;
		display: grid;
		gap: 10px;
		border-color: color-mix(in srgb, var(--ai) 25%, var(--line));
		padding: 14px 16px;
		max-height: calc(100vh - 96px);
		overflow: auto;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.spark {
		color: var(--ai);
		font-size: 16px;
	}
	.grow {
		flex: 1;
	}
	.q {
		width: 100%;
		resize: vertical;
		box-sizing: border-box;
	}
	.examples {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.ex {
		font: inherit;
		font-size: 12px;
		text-align: left;
		background: var(--ai-soft);
		color: var(--ai);
		border: 0;
		border-radius: 8px;
		padding: 4px 8px;
		cursor: pointer;
	}
	.ex:disabled {
		opacity: 0.55;
		cursor: default;
	}
	.attach {
		position: relative;
		overflow: hidden;
	}
	.attach input {
		position: absolute;
		inset: 0;
		opacity: 0;
		cursor: pointer;
	}
	.attach.disabled {
		opacity: 0.5;
	}
	.docs {
		margin: 0;
	}
	.answer p {
		margin: 0 0 6px;
	}
	.answer .bullet::before {
		content: '• ';
		color: var(--ai);
	}
	.sg {
		border: 1px solid var(--line);
		border-radius: 10px;
		padding: 10px 12px;
		display: grid;
		gap: 6px;
		background: var(--card);
	}
	.sg.done {
		opacity: 0.7;
	}
	.sg p {
		margin: 0;
	}
	.t {
		font-size: 13.5px;
	}
	.ev {
		margin: 0;
		padding: 0;
		list-style: none;
		font-size: 12px;
		color: var(--muted);
	}
	.ev li span {
		color: var(--ok);
		font-weight: 700;
	}
	.ev li.nf span {
		color: var(--bad);
	}
	.warnline {
		color: var(--warn);
	}
	.qs {
		background: var(--bg);
		border-radius: 8px;
		padding: 8px 10px;
	}
	.qs p {
		margin: 2px 0 0;
	}
</style>
