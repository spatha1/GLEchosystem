<script lang="ts">
	// ALTB-06 — the Ledger, sandbox first. Separate from the configuration studio: a book pins one
	// approved configuration version and posts synthetic events through the same engine as the
	// studio's previews. Every book is a sandbox (enforced by the database): nothing here is real
	// financial data. Operators open books and post; everyone else reads.
	import type { ApiError, Client } from '../lib/api';
	import type { Me } from '../lib/types';
	import type { Route } from '../lib/route';
	import type { Account, JournalEntry, LifecycleProfile, SandboxBook, SandboxBookView, SandboxPostResult, Scenario } from '../lib/studio';
	import { amount, when } from '../lib/format';

	let { api, me, book: bookId, go }: { api: Client; me: Me; book?: string; go: (r: Route) => void } = $props();

	const canPost = $derived(me.roles.includes('OPERATOR'));

	// ── list ──
	let books = $state<SandboxBook[] | null>(null);
	let failure = $state('');
	let opening = $state(false);
	let choices = $state<{ configId: string; version: number }[]>([]);
	let form = $state({ bookId: '', target: '', reason: '' });
	let formError = $state('');
	let busy = $state(false);

	// ── one book ──
	let view = $state<SandboxBookView | null>(null);
	let scenarios = $state<Scenario[]>([]);
	let accounts = $state<Account[]>([]);
	let profiles = $state<LifecycleProfile[]>([]);
	let steps = $state<string[]>([]);
	let adding = $state('');
	let result = $state<SandboxPostResult | null>(null);
	let postError = $state('');
	let openSeq = $state<number | null>(null);

	const acctName = (code: string) => accounts.find((a) => a.code === code)?.name ?? '';
	const errText = (e: unknown) => {
		const err = e as ApiError;
		return `${err.code}: ${err.message}`;
	};

	$effect(() => {
		if (bookId) return;
		failure = '';
		api.sandboxBooks().then(
			(b) => (books = b),
			(e) => (failure = errText(e))
		);
	});

	$effect(() => {
		if (!bookId) return;
		const id = bookId;
		view = null;
		failure = '';
		result = null;
		steps = [];
		api.sandboxBook(id).then(
			async (v) => {
				if (id !== bookId) return;
				view = v;
				try {
					const ver = await api.version(v.book.configId, v.book.configVersion);
					const content = ver.content as { scenarios?: Scenario[]; accounts?: Account[]; lifecycleProfiles?: LifecycleProfile[] };
					scenarios = content.scenarios ?? [];
					profiles = content.lifecycleProfiles ?? [];
					accounts = content.accounts ?? [];
				} catch {
					scenarios = [];
				}
			},
			(e) => (failure = errText(e))
		);
	});

	async function startOpening() {
		opening = true;
		formError = '';
		const out: { configId: string; version: number }[] = [];
		try {
			for (const c of await api.configs()) {
				for (const v of await api.versions(c.configId)) if (v.status === 'approved') out.push({ configId: c.configId, version: v.version });
			}
		} catch (e) {
			formError = errText(e);
		}
		choices = out;
	}

	async function openBook() {
		const [configId, version] = form.target.split('@');
		busy = true;
		formError = '';
		try {
			const b = await api.openSandboxBook(form.bookId.trim().toUpperCase(), configId, Number(version), form.reason.trim());
			opening = false;
			form = { bookId: '', target: '', reason: '' };
			go({ ledger: true, book: b.bookId });
		} catch (e) {
			formError = errText(e);
		} finally {
			busy = false;
		}
	}

	function add() {
		if (!adding) return;
		steps = [...steps, adding];
		adding = '';
	}
	function move(i: number, by: number) {
		const j = i + by;
		if (j < 0 || j >= steps.length) return;
		const next = [...steps];
		[next[i], next[j]] = [next[j], next[i]];
		steps = next;
	}

	async function post() {
		if (!view) return;
		busy = true;
		postError = '';
		result = null;
		try {
			result = await api.postToSandbox(view.book.bookId, steps.map((scenario) => ({ scenario })));
			steps = [];
			view = await api.sandboxBook(view.book.bookId);
		} catch (e) {
			postError = errText(e);
		} finally {
			busy = false;
		}
	}

	// Trial balance totals per legal entity and currency.
	const totals = $derived.by(() => {
		const m = new Map<string, { entity: string; currency: string; dr: number; cr: number }>();
		for (const b of view?.balances ?? []) {
			const k = `${b.entity} ${b.currency}`;
			const t = m.get(k) ?? { entity: b.entity, currency: b.currency, dr: 0, cr: 0 };
			const bal = Number(b.balance);
			if (bal >= 0) t.dr += bal;
			else t.cr -= bal;
			m.set(k, t);
		}
		return [...m.values()];
	});
	const lineCount = (js: JournalEntry[]) => js.reduce((n, j) => n + j.lines.length, 0);
</script>

<div class="ledger">
	<div class="notice info sandbox" role="note">
		<b>Sandbox ledger · synthetic data only.</b> Every book here is a sandbox, enforced by the database: nothing is real financial data and nothing reaches a
		real ledger. Books post through the same engine as the studio's previews.
	</div>
	{#if failure}<div class="notice bad" role="alert">{failure}</div>{/if}

	{#if !bookId}
		<section class="card">
			<div class="row">
				<h3 class="grow">Sandbox books</h3>
				{#if canPost && !opening}<button class="btn primary" onclick={startOpening}>Open a sandbox book</button>{/if}
			</div>
			{#if !canPost}<p class="small muted">Only an operator opens books and posts to them; you can read them.</p>{/if}
			{#if opening}
				<form
					class="open"
					aria-label="Open a sandbox book"
					onsubmit={(e) => {
						e.preventDefault();
						openBook();
					}}
				>
					<label class="lbl">Book id <input class="field mono" bind:value={form.bookId} placeholder="e.g. SANDBOX_JAN" required /></label>
					<label class="lbl"
						>Posts with (approved versions only)
						<select class="field" bind:value={form.target} required>
							<option value="">Choose…</option>
							{#each choices as c (`${c.configId}@${c.version}`)}<option value="{c.configId}@{c.version}">{c.configId} v{c.version}</option>{/each}
						</select>
					</label>
					<label class="lbl wide">Why <input class="field" bind:value={form.reason} placeholder="What this sandbox run is for" required /></label>
					<div class="row">
						<button class="btn primary" type="submit" disabled={busy || !form.bookId.trim() || !form.target || !form.reason.trim()}>Open book</button>
						<button class="btn" type="button" onclick={() => (opening = false)}>Cancel</button>
					</div>
					{#if formError}<div class="notice bad" role="alert">{formError}</div>{/if}
				</form>
			{/if}
			{#if books && books.length}
				<table class="data">
					<thead><tr><th>Book</th><th>Posts with</th><th>Postings</th><th>Opened</th><th>Why</th></tr></thead>
					<tbody>
						{#each books as b (b.bookId)}
							<tr>
								<td><button class="btn link mono" onclick={() => go({ ledger: true, book: b.bookId })}>{b.bookId}</button></td>
								<td class="mono">{b.configId} v{b.configVersion}</td>
								<td>{b.postings}</td>
								<td class="small">{b.createdBy} · {when(b.createdAt)}</td>
								<td class="small">{b.reason}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{:else if books}
				<p class="small muted">No sandbox books yet.</p>
			{/if}
		</section>
	{:else if view}
		<section class="card">
			<div class="row">
				<h3 class="grow">Book <span class="mono">{view.book.bookId}</span> <span class="badge info">sandbox</span></h3>
				<button class="btn link" onclick={() => go({ ledger: true })}>All books</button>
			</div>
			<p class="small">
				Posts with
				<button class="btn link mono" onclick={() => go({ configId: view!.book.configId, version: view!.book.configVersion })}
					>{view.book.configId} v{view.book.configVersion}</button
				>
				(approved, pinned) · opened by {view.book.createdBy} {when(view.book.createdAt)} · {view.book.reason}
			</p>
		</section>

		{#if canPost}
			<section class="card" aria-label="Post synthetic events">
				<h3>Post synthetic events</h3>
				<p class="small muted">
					Events post in the order below, all or nothing: if one cannot post, nothing is stored and the reason is shown. Posting an event again with the same
					content changes nothing. Preview first in the studio (Simulate → Lifecycle preview).
				</p>
				{#if profiles.length}
					<div class="row pick">
						<label class="lbl"
							>Start from a profile
							<select
								class="field"
								value=""
								onchange={(e) => {
									const p = profiles.find((x) => x.code === (e.currentTarget as HTMLSelectElement).value);
									if (p) steps = [...p.steps];
									(e.currentTarget as HTMLSelectElement).value = '';
								}}
							>
								<option value="">Choose…</option>
								{#each profiles as p (p.code)}<option value={p.code}>{p.code}{p.description ? ` — ${p.description}` : ''} ({p.steps.length} steps)</option>{/each}
							</select>
						</label>
					</div>
				{/if}
				<div class="row pick">
					<label class="lbl"
						>Add a scenario of {view.book.configId} v{view.book.configVersion}
						<select class="field" bind:value={adding}>
							<option value="">Choose…</option>
							{#each scenarios as s (s.code)}<option value={s.code}>{s.code} · {s.eventType}{s.description ? ` — ${s.description}` : ''}</option>{/each}
						</select>
					</label>
					<button class="btn" onclick={add} disabled={!adding}>Add</button>
					<span class="spacer"></span>
					<button class="btn primary" onclick={post} disabled={!steps.length || busy}>{busy ? 'Posting…' : 'Post to sandbox'}</button>
				</div>
				{#if steps.length}
					<ol class="steps">
						{#each steps as code, i (i)}
							<li>
								<span class="mono"><b>{code}</b></span>
								<button class="btn link" onclick={() => move(i, -1)} disabled={i === 0} aria-label="Move {code} up">↑</button>
								<button class="btn link" onclick={() => move(i, 1)} disabled={i === steps.length - 1} aria-label="Move {code} down">↓</button>
								<button class="btn link" onclick={() => (steps = steps.filter((_, j) => j !== i))} aria-label="Remove {code}">Remove</button>
							</li>
						{/each}
					</ol>
				{/if}
				{#if postError}<div class="notice bad" role="alert">{postError}</div>{/if}
				{#if result}
					<div class="notice ok" role="status">
						Posted {result.posted} event{result.posted === 1 ? '' : 's'}{#if result.steps.some((s) => s.status === 'UNCHANGED')}; unchanged (already posted):
							{result.steps
								.filter((s) => s.status === 'UNCHANGED')
								.map((s) => s.code)
								.join(', ')}{/if}.
					</div>
				{/if}
			</section>
		{/if}

		<section class="card">
			<h3>Postings <span class="muted">({view.postings.length})</span></h3>
			{#if view.postings.length}
				<table class="data">
					<thead><tr><th>#</th><th>Step</th><th>Event</th><th>Source event</th><th>Economic event</th><th>Book date</th><th>Entries</th><th>Posted</th><th></th></tr></thead>
					<tbody>
						{#each view.postings as p (p.seq)}
							<tr class:on={openSeq === p.seq}>
								<td>{p.seq}</td>
								<td class="mono"><b>{p.code ?? ''}</b></td>
								<td class="mono">{p.eventType}</td>
								<td class="mono small">{p.sourceSystem}/{p.sourceEventId} v{p.sourceEventVersion}</td>
								<td class="mono small">{p.economicEventKey}</td>
								<td>{p.bookDate}</td>
								<td>{p.journals.length} · {lineCount(p.journals)} lines</td>
								<td class="small">{p.postedBy}</td>
								<td><button class="btn link" onclick={() => (openSeq = openSeq === p.seq ? null : p.seq)}>{openSeq === p.seq ? 'Hide' : 'Journals'}</button></td>
							</tr>
							{#if openSeq === p.seq}
								<tr class="jrow">
									<td colspan="9">
										{#each p.journals as j (j.journalId)}
											<div class="journal">
												<div class="small"><b class="mono">{j.journalId}</b> · {j.legalEntity} {j.currency} · period {j.accountingPeriod} · {j.status} · rule {j.lines[0]?.ruleId} v{j.configVersion}</div>
												<table class="data">
													<thead><tr><th>Line</th><th>Account</th><th>Class</th><th>Subledger</th><th class="num">Debit</th><th class="num">Credit</th></tr></thead>
													<tbody>
														{#each j.lines as l (l.lineId)}
															<tr>
																<td class="mono small">{l.lineId.slice(j.journalId.length + 1)}</td>
																<td><span class="mono"><b>{l.account}</b></span> <span class="small muted">{acctName(l.account)}</span></td>
																<td class="mono">{l.accountingClass}</td>
																<td class="mono small">{l.subledgerKey ?? '—'}</td>
																<td class="num">{l.side === 'D' ? amount(l.amount) : ''}</td>
																<td class="num">{l.side === 'C' ? amount(l.amount) : ''}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											</div>
										{/each}
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			{:else}
				<p class="small muted">Nothing posted yet.</p>
			{/if}
		</section>

		<section class="card" aria-label="Trial balance">
			<h3>Trial balance <span class="muted">— by legal entity, account and subledger key</span></h3>
			{#if view.balances.length}
				<table class="data">
					<thead><tr><th>Entity</th><th>Account</th><th>Subledger</th><th class="num">Debit</th><th class="num">Credit</th><th class="num" title="Debit minus credit">Balance (Dr − Cr)</th></tr></thead>
					<tbody>
						{#each view.balances as b (`${b.entity} ${b.currency} ${b.account} ${b.value ?? ''}`)}
							<tr>
								<td class="mono">{b.entity} <span class="muted">{b.currency}</span></td>
								<td><span class="mono"><b>{b.account}</b></span> <span class="small muted">{acctName(b.account)}</span></td>
								<td class="mono small">{b.key ? `${b.key} ${b.value}` : '—'}</td>
								<td class="num">{amount(b.debit)}</td>
								<td class="num">{amount(b.credit)}</td>
								<td class="num"><b>{amount(b.balance)}</b></td>
							</tr>
						{/each}
					</tbody>
				</table>
				<div class="sum">
					{#each totals as t (`${t.entity} ${t.currency}`)}
						<span class="badge {Math.abs(t.dr - t.cr) < 0.005 ? 'ok' : 'bad'}"
							>{t.entity} {t.currency}: debit balances {amount(t.dr.toFixed(2))} = credit balances {amount(t.cr.toFixed(2))}</span
						>
					{/each}
				</div>
			{:else}
				<p class="small muted">No balances yet.</p>
			{/if}
		</section>
	{/if}
</div>

<style>
	.ledger {
		display: grid;
		gap: 14px;
	}
	.grow {
		flex: 1;
	}
	.open {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 10px;
		margin: 10px 0 14px;
		align-items: end;
	}
	.open .wide {
		grid-column: 1 / -1;
	}
	.pick {
		align-items: end;
		margin: 6px 0 10px;
	}
	.pick .lbl {
		flex: 0 1 480px;
	}
	.steps {
		margin: 0 0 10px;
		padding-left: 22px;
	}
	tr.on td {
		background: var(--gl-accent-soft);
	}
	.jrow > td {
		background: var(--gl-surface-2);
	}
	.journal + .journal {
		margin-top: 10px;
	}
	.sum {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 10px;
	}
	.ledger :global(table.data) {
		width: 100%;
	}
</style>
