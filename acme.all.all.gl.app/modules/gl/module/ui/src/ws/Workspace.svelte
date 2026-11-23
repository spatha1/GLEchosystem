<svelte:options
	customElement={{
		tag: 'gl-workspace',
		props: { clients: { type: 'String', attribute: 'clients' }, hashRouting: { type: 'Boolean', attribute: 'hash-routing' }, studioUrl: { type: 'String', attribute: 'studio-url' } }
	}}
/>

<script lang="ts">
	// The GL Ecosystem workspace: Administration → Client onboarding → Client workspace → Stage 1
	// (chart of accounts), with the AI assistant on every page. A separate element from the
	// classic studio (<gl-app>), which stays as it is; both use the same API and rules.
	import { wsClient, roleLabels, type WsClient } from './api';
	import { parseClients, parseHash, toHash, STAGE1_STEPS, type WsRoute } from './route';
	import type { Me } from '../lib/types';
	import AdminPage from './AdminPage.svelte';
	import OnboardingPage from './OnboardingPage.svelte';
	import ClientPage from './ClientPage.svelte';
	import Stage1 from './stage1/Stage1.svelte';

	let { clients = '', hashRouting = false, studioUrl = 'index.html' }: { clients?: string; hashRouting?: boolean; studioUrl?: string } = $props();

	const list = $derived(parseClients(clients));
	let route = $state<WsRoute>({ page: 'home' });
	let mine = $state<Record<string, Me | null>>({}); // my roles per client (null: no access)
	let loaded = $state(false);

	$effect(() => {
		const l = list;
		loaded = false;
		Promise.all(
			l.map((c) =>
				wsClient(c.code)
					.me()
					.then(
						(m) => [c.code, m] as const,
						() => [c.code, null] as const
					)
			)
		).then((rs) => {
			mine = Object.fromEntries(rs);
			loaded = true;
		});
	});

	$effect(() => {
		if (!hashRouting) return;
		const sync = () => (route = parseHash(location.hash));
		sync();
		window.addEventListener('hashchange', sync);
		return () => window.removeEventListener('hashchange', sync);
	});

	function go(r: WsRoute) {
		if (hashRouting) {
			const h = toHash(r);
			if (location.hash !== h) location.hash = h;
			else route = r;
		} else route = r;
		queueMicrotask(() => (document.querySelector('gl-workspace')?.shadowRoot?.querySelector('main') as HTMLElement | null)?.scrollTo?.({ top: 0 }));
	}

	const actor = $derived(Object.values(mine).find((m) => m)?.actor ?? '');
	const rolesIn = (c?: string) => (c ? (mine[c]?.roles ?? []) : []);
	const adminOf = $derived(list.filter((c) => rolesIn(c.code).includes('GL_ADMIN')));
	const memberOf = $derived(list.filter((c) => rolesIn(c.code).length));
	const current = $derived(route.client && mine[route.client] ? route.client : (memberOf[0]?.code ?? ''));
	const api = $derived<WsClient | null>(current ? wsClient(current) : null);
	const label = (c: string) => list.find((x) => x.code === c)?.label ?? c;

	// home: administrators start in administration, everyone else in their client's workspace
	const page = $derived(route.page === 'home' ? (adminOf.length ? 'admin' : 'client') : route.page);

	function pickClient(code: string) {
		go({ ...route, page: page as WsRoute['page'], client: code });
	}
</script>

<div class="shell">
	<header class="top">
		<button class="brand" onclick={() => go({ page: 'home' })} aria-label="GL Ecosystem home"><span class="logo">GL</span> Ecosystem</button>
		{#if memberOf.length}
			<label class="client-pick"
				><span>Client</span>
				<select value={current} onchange={(e) => pickClient((e.currentTarget as HTMLSelectElement).value)} aria-label="Client">
					{#each memberOf as c (c.code)}<option value={c.code}>{c.label}</option>{/each}
				</select></label
			>
		{/if}
		<span class="grow"></span>
		{#if actor}<span class="who" title={actor}><span class="avatar">{actor.slice(0, 1).toUpperCase()}</span>{actor}<small>{rolesIn(current).map((r) => roleLabels[r] ?? r).join(' · ') || 'no role here'}</small></span>{/if}
		<a class="classic" href={studioUrl} title="The classic configuration studio (unchanged)">Classic studio ↗</a>
	</header>

	<nav class="side" aria-label="Workspace">
		{#if adminOf.length}
			<button class:on={page === 'admin'} onclick={() => go({ page: 'admin', client: current })}><span class="ic">⚙</span> Administration</button>
			<button class:on={page === 'onboard'} onclick={() => go({ page: 'onboard', client: current })}><span class="ic">＋</span> Client onboarding</button>
		{/if}
		{#if memberOf.length}
			<button class:on={page === 'client'} onclick={() => go({ page: 'client', client: current })}><span class="ic">▦</span> Client workspace</button>
			<div class="group">Stage 1 · Chart of accounts</div>
			{#each STAGE1_STEPS as s (s.n)}
				<button class="sub" class:on={page === 'stage1' && route.step === s.n} onclick={() => go({ page: 'stage1', client: current, step: s.n })}
					><span class="n">{s.n}</span> {s.title}</button
				>
			{/each}
			<div class="group muted">Stage 2 · Events and posting rules</div>
			<span class="soon">Next stage — not in this release</span>
		{/if}
	</nav>

	<main>
		{#if !loaded}
			<div class="empty">Loading…</div>
		{:else if !memberOf.length}
			<div class="empty card">
				<h2>No access yet</h2>
				<p>You have no role in any client. Ask your client's GL administrator for access.</p>
			</div>
		{:else if page === 'admin'}
			<AdminPage {list} {mine} client={current} {go} />
		{:else if page === 'onboard'}
			<OnboardingPage {list} {mine} client={current} {go} />
		{:else if page === 'client' && api}
			{#key current}<ClientPage {api} me={mine[current]!} clientLabel={label(current)} {go} {studioUrl} />{/key}
		{:else if page === 'stage1' && api}
			{#key current}<Stage1 {api} me={mine[current]!} clientLabel={label(current)} step={route.step ?? 1} {go} />{/key}
		{/if}
	</main>
</div>

<style>
	:host {
		--bg: #f4f6fb;
		--card: #ffffff;
		--text: #1a2133;
		--muted: #5e697e;
		--faint: #8a94a8;
		--line: #e2e7f0;
		--accent: #2346a0;
		--accent-2: #3b63d6;
		--accent-soft: #eaf0ff;
		--ok: #1d7a4a;
		--ok-soft: #e6f6ed;
		--warn: #9a6700;
		--warn-soft: #fff5dc;
		--bad: #b4232c;
		--bad-soft: #fdecec;
		--ai: #6d3fd6;
		--ai-soft: #f2ecff;
		--radius: 12px;
		--shadow: 0 1px 2px rgba(16, 24, 40, 0.05), 0 1px 3px rgba(16, 24, 40, 0.06);
		display: block;
		font: 14px/1.5 Inter, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
		color: var(--text);
	}
	@media (prefers-color-scheme: dark) {
		:host {
			--bg: #0f1420;
			--card: #171e2d;
			--text: #e6e9f0;
			--muted: #a3adbf;
			--faint: #7d879a;
			--line: #283247;
			--accent: #8fb0ff;
			--accent-2: #a8c1ff;
			--accent-soft: #1e2a47;
			--ok: #5fd39b;
			--ok-soft: #15301f;
			--warn: #f0c060;
			--warn-soft: #33290f;
			--bad: #ff8a8a;
			--bad-soft: #3a1a1c;
			--ai: #b69cff;
			--ai-soft: #261d3f;
		}
	}
	.shell {
		display: grid;
		grid-template-columns: 248px 1fr;
		grid-template-rows: 56px 1fr;
		min-height: 100vh;
		background: var(--bg);
	}
	.top {
		grid-column: 1 / -1;
		display: flex;
		align-items: center;
		gap: 16px;
		padding: 0 18px;
		background: var(--card);
		border-bottom: 1px solid var(--line);
		position: sticky;
		top: 0;
		z-index: 5;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 8px;
		font-weight: 700;
		font-size: 16px;
		background: none;
		border: 0;
		color: var(--text);
		cursor: pointer;
	}
	.logo {
		background: linear-gradient(135deg, var(--accent), var(--ai));
		color: #fff;
		border-radius: 8px;
		padding: 2px 7px;
		font-size: 13px;
	}
	.client-pick {
		display: flex;
		align-items: center;
		gap: 8px;
		color: var(--muted);
		font-size: 13px;
	}
	.client-pick select {
		font: inherit;
		font-weight: 600;
		color: var(--text);
		background: var(--bg);
		border: 1px solid var(--line);
		border-radius: 8px;
		padding: 5px 10px;
	}
	.grow {
		flex: 1;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
	}
	.who small {
		color: var(--muted);
	}
	.avatar {
		width: 28px;
		height: 28px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
	}
	.classic {
		font-size: 13px;
		color: var(--muted);
		text-decoration: none;
		border: 1px solid var(--line);
		border-radius: 8px;
		padding: 5px 10px;
	}
	.side {
		background: var(--card);
		border-right: 1px solid var(--line);
		padding: 14px 10px;
		display: flex;
		flex-direction: column;
		gap: 2px;
		position: sticky;
		top: 56px;
		height: calc(100vh - 56px);
		overflow: auto;
	}
	.side button {
		text-align: left;
		font: inherit;
		background: none;
		border: 0;
		border-radius: 8px;
		padding: 8px 10px;
		color: var(--text);
		cursor: pointer;
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.side button:hover {
		background: var(--bg);
	}
	.side button.on {
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 600;
	}
	.side .ic {
		width: 18px;
		text-align: center;
		opacity: 0.8;
	}
	.side .sub {
		padding-left: 14px;
		font-size: 13px;
	}
	.side .n {
		width: 20px;
		height: 20px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-size: 11px;
		background: var(--bg);
		border: 1px solid var(--line);
	}
	.side .on .n {
		background: var(--accent);
		color: #fff;
		border-color: var(--accent);
	}
	.group {
		margin: 14px 10px 4px;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--faint);
		font-weight: 600;
	}
	.soon {
		font-size: 12px;
		color: var(--faint);
		padding: 2px 12px;
	}
	main {
		padding: 22px 26px 40px;
		min-width: 0;
	}
	.empty {
		color: var(--muted);
		padding: 30px;
	}
	@media (max-width: 860px) {
		.shell {
			grid-template-columns: 1fr;
			grid-template-rows: auto auto 1fr;
		}
		.side {
			position: static;
			height: auto;
			flex-direction: row;
			flex-wrap: wrap;
		}
		.group,
		.soon {
			display: none;
		}
		.top {
			flex-wrap: wrap;
			padding: 8px 12px;
		}
		main {
			padding: 16px;
		}
	}

	/* shared building blocks of every workspace page */
	:global(.card) {
		background: var(--card);
		border: 1px solid var(--line);
		border-radius: var(--radius);
		box-shadow: var(--shadow);
		padding: 18px 20px;
	}
	:global(.ws-h1) {
		font-size: 22px;
		margin: 0 0 4px;
		font-weight: 700;
	}
	:global(.ws-lead) {
		color: var(--muted);
		margin: 0 0 18px;
		max-width: 860px;
	}
	:global(.btn) {
		font: inherit;
		font-weight: 600;
		font-size: 13px;
		border: 1px solid var(--line);
		background: var(--card);
		color: var(--text);
		border-radius: 8px;
		padding: 7px 14px;
		cursor: pointer;
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}
	:global(.btn:hover:not(:disabled)) {
		border-color: var(--accent);
		color: var(--accent);
	}
	:global(.btn:disabled) {
		opacity: 0.5;
		cursor: not-allowed;
	}
	:global(.btn.primary) {
		background: var(--accent);
		border-color: var(--accent);
		color: #fff;
	}
	:global(.btn.primary:hover:not(:disabled)) {
		background: var(--accent-2);
		color: #fff;
	}
	:global(.btn.ai) {
		background: var(--ai-soft);
		border-color: transparent;
		color: var(--ai);
	}
	:global(.btn.ghost) {
		border-color: transparent;
		background: none;
		color: var(--muted);
	}
	:global(.btn.small) {
		padding: 4px 10px;
		font-size: 12px;
	}
	:global(.btn.danger) {
		color: var(--bad);
	}
	:global(.row) {
		display: flex;
		gap: 10px;
		align-items: center;
		flex-wrap: wrap;
	}
	:global(.stack) {
		display: grid;
		gap: 16px;
	}
	:global(.muted) {
		color: var(--muted);
	}
	:global(.small) {
		font-size: 12px;
	}
	:global(.mono) {
		font-family: ui-monospace, 'Cascadia Code', Consolas, monospace;
		font-size: 12.5px;
	}
	:global(.pill) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		border-radius: 999px;
		padding: 1px 9px;
		font-size: 12px;
		font-weight: 600;
		background: var(--bg);
		color: var(--muted);
		border: 1px solid var(--line);
		white-space: nowrap;
	}
	:global(.pill.ok) {
		background: var(--ok-soft);
		color: var(--ok);
		border-color: transparent;
	}
	:global(.pill.warn) {
		background: var(--warn-soft);
		color: var(--warn);
		border-color: transparent;
	}
	:global(.pill.bad) {
		background: var(--bad-soft);
		color: var(--bad);
		border-color: transparent;
	}
	:global(.pill.info) {
		background: var(--accent-soft);
		color: var(--accent);
		border-color: transparent;
	}
	:global(.pill.ai) {
		background: var(--ai-soft);
		color: var(--ai);
		border-color: transparent;
	}
	:global(.notice) {
		border-radius: 10px;
		padding: 10px 14px;
		background: var(--bg);
		border: 1px solid var(--line);
	}
	:global(.notice.ok) {
		background: var(--ok-soft);
		border-color: transparent;
		color: var(--ok);
	}
	:global(.notice.warn) {
		background: var(--warn-soft);
		border-color: transparent;
		color: var(--warn);
	}
	:global(.notice.bad) {
		background: var(--bad-soft);
		border-color: transparent;
		color: var(--bad);
	}
	:global(.notice.ai) {
		background: var(--ai-soft);
		border-color: transparent;
		color: var(--ai);
	}
	:global(.field) {
		font: inherit;
		color: var(--text);
		background: var(--card);
		border: 1px solid var(--line);
		border-radius: 8px;
		padding: 7px 10px;
		min-width: 0;
	}
	:global(.field:focus) {
		outline: 2px solid var(--accent-soft);
		border-color: var(--accent);
	}
	:global(.lbl) {
		display: grid;
		gap: 4px;
		font-size: 12px;
		font-weight: 600;
		color: var(--muted);
	}
	:global(table.grid) {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	:global(table.grid th) {
		text-align: left;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--faint);
		font-weight: 600;
		padding: 8px 10px;
		border-bottom: 1px solid var(--line);
		background: var(--card);
		position: sticky;
		top: 0;
	}
	:global(table.grid td) {
		padding: 8px 10px;
		border-bottom: 1px solid var(--line);
		vertical-align: top;
	}
	:global(table.grid tr.click:hover td) {
		background: var(--bg);
		cursor: pointer;
	}
	:global(table.grid tr.sel td) {
		background: var(--accent-soft);
	}
	:global(.tw) {
		overflow: auto;
		max-height: 560px;
		border: 1px solid var(--line);
		border-radius: 10px;
	}
	:global(.with-ai) {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 360px;
		gap: 18px;
		align-items: start;
	}
	@media (max-width: 1180px) {
		:global(.with-ai) {
			grid-template-columns: 1fr;
		}
	}
	:global(.kpis) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 12px;
	}
	:global(.kpi) {
		background: var(--card);
		border: 1px solid var(--line);
		border-radius: 10px;
		padding: 12px 14px;
	}
	:global(.kpi b) {
		display: block;
		font-size: 22px;
	}
	:global(.kpi span) {
		color: var(--muted);
		font-size: 12px;
	}
	:global(h2.sec) {
		font-size: 15px;
		margin: 0 0 10px;
	}
</style>
