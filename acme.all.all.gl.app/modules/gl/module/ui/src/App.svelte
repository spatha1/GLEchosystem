<svelte:options
	customElement={{
		tag: 'gl-app',
		props: { tenant: { type: 'String', attribute: 'tenant' }, hashRouting: { type: 'Boolean', attribute: 'hash-routing' } }
	}}
/>

<script lang="ts">
	import { client, type ApiError } from './lib/api';
	import type { Me } from './lib/types';
	import { fromHash, toHash, type Route } from './lib/route';
	import { versionViews, viewOf } from './lib/views';
	import { icon } from './components/icons';
	import Home from './studio/Home.svelte';
	import VersionScope from './studio/VersionScope.svelte';
	import LedgerSandbox from './ledger/LedgerSandbox.svelte';

	let { tenant = '', hashRouting = false }: { tenant?: string; hashRouting?: boolean } = $props();

	const roleNames: Record<string, string> = {
		GL_ADMIN: 'Administrator',
		CONFIG_EDITOR: 'Editor',
		CONFIG_APPROVER: 'Approver',
		OPERATOR: 'Operator',
		VIEWER: 'Viewer'
	};

	let me = $state<Me | null>(null);
	let failure = $state<ApiError | null>(null);
	let route = $state<Route>({});
	const api = $derived(client(tenant));

	$effect(() => {
		const a = api;
		me = null;
		failure = null;
		if (!tenant) return;
		a.me().then(
			(m) => (me = m),
			(e) => (failure = e)
		);
	});

	// Optional URL routing: the view is linkable and the back button works.
	$effect(() => {
		if (!hashRouting) return;
		const sync = () => (route = fromHash(location.hash));
		sync();
		window.addEventListener('hashchange', sync);
		return () => window.removeEventListener('hashchange', sync);
	});

	function go(r: Route) {
		drawer = false;
		if (hashRouting) {
			const h = toHash(r);
			if (location.hash !== h) location.hash = h;
			else route = r;
		} else {
			route = r;
		}
		queueMicrotask(() => (document.querySelector('gl-app')?.shadowRoot?.querySelector('main') as HTMLElement | null)?.scrollIntoView?.({ block: 'start' }));
	}

	// Per-viewer conveniences, kept in this browser only: the colour theme and the narrow navigation rail.
	const read = (k: string) => {
		try {
			return localStorage.getItem(k) ?? '';
		} catch {
			return '';
		}
	};
	const write = (k: string, v: string) => {
		try {
			localStorage.setItem(k, v);
		} catch {
			/* storage unavailable: the choice lasts for this page only */
		}
	};
	let theme = $state(read('gl_theme')); // '', 'light' or 'dark' ('' follows the system)
	let railed = $state(read('gl_nav') === 'rail');
	let drawer = $state(false);
	let narrow = $state(false);
	let systemDark = $state(false);
	$effect(() => {
		const n = matchMedia('(max-width: 1023px)');
		const d = matchMedia('(prefers-color-scheme: dark)');
		const sync = () => ((narrow = n.matches), (systemDark = d.matches));
		sync();
		n.addEventListener('change', sync);
		d.addEventListener('change', sync);
		return () => (n.removeEventListener('change', sync), d.removeEventListener('change', sync));
	});
	const dark = $derived(theme ? theme === 'dark' : systemDark);
	function toggleTheme() {
		theme = dark ? 'light' : 'dark';
		write('gl_theme', theme);
	}
	function toggleNav() {
		if (narrow) drawer = !drawer;
		else {
			railed = !railed;
			write('gl_nav', railed ? 'rail' : 'expanded');
		}
	}
	const expanded = $derived(narrow || !railed);

	const inVersion = $derived(!!(route.configId && route.version));
	const view = $derived(viewOf(route.tab));
	const crumbs = $derived.by(() => {
		if (route.ledger) {
			const l: { label: string; to?: Route; mono?: boolean }[] = [{ label: 'Ledger · sandbox', to: { ledger: true } }];
			if (route.book) l.push({ label: route.book, to: { ledger: true, book: route.book }, mono: true });
			return l;
		}
		const out: { label: string; to?: Route; mono?: boolean }[] = [{ label: 'Configurations', to: {} }];
		if (route.configId) out.push({ label: route.configId, to: { configId: route.configId }, mono: true });
		if (inVersion) {
			const base = { configId: route.configId, version: route.version };
			out.push({ label: `v${route.version}`, to: base, mono: true });
			out.push({ label: route.event ? 'Business events' : view.label, to: { ...base, tab: route.event ? 'events' : route.tab } });
			if (route.event) out.push({ label: route.event, to: { ...base, event: route.event }, mono: true });
			if (route.event && route.rule)
				out.push({ label: route.rule === '+new' ? 'New rule' : route.rule, to: { ...route, edit: false, scenario: undefined }, mono: route.rule !== '+new' });
		}
		return out;
	});

	// On a phone the breadcrumb strip scrolls: keep the current page in view.
	let crumbBar = $state<HTMLElement | null>(null);
	$effect(() => {
		crumbs;
		const el = crumbBar;
		if (el) queueMicrotask(() => (el.scrollLeft = el.scrollWidth));
	});

	const initials = $derived(
		(me?.actor ?? '')
			.split('@')[0]
			.split(/[^A-Za-z0-9]+/)
			.filter(Boolean)
			.map((w, _i, all) => (all.length === 1 ? w.slice(0, 2) : w[0]))
			.join('')
			.slice(0, 2)
			.toUpperCase() || '··'
	);
</script>

<div class="app" data-theme={theme || undefined} class:dark>
	{#if !tenant}
		<div class="state">No client is selected.</div>
	{:else if failure}
		<div class="state bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
	{:else if !me}
		<div class="state">Loading…</div>
	{:else if me.roles.length === 0}
		<div class="state warn" role="alert">You have no role for client {me.tenantId}. Ask a GL administrator for access.</div>
	{:else}
		<div class="shell" class:railed={!expanded} class:narrow>
			<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
			<div class="scrim" class:open={drawer && narrow} onclick={() => (drawer = false)}></div>
			<nav class="side" class:open={drawer} aria-label="Studio">
				<div class="brand">
					<button class="brandlink" onclick={() => go({})} title="GL Configuration Studio">
						<span class="mark" aria-hidden="true">GL</span>
						{#if expanded}<span class="bname">GL Configuration Studio</span>{/if}
					</button>
					{#if narrow}<button class="iconbtn" aria-label="Close navigation" onclick={() => (drawer = false)}>{@html icon('close')}</button>{/if}
				</div>

				<div class="navscroll">
					{#if expanded}<div class="area">Client {me.tenantId}</div>{/if}
					{#if expanded}<div class="sect" class:current={!inVersion}>1. Configurations</div>{/if}
					<button class="item" data-selected={!inVersion && !route.ledger} aria-current={!inVersion && !route.ledger ? 'page' : undefined} title="All configurations" onclick={() => go({})}>
						{@html icon('configs')}<span class="lbl">{expanded ? 'All configurations' : 'Configs'}</span>
					</button>

					{#if inVersion}
						{#if expanded}<div class="sect current" title="{route.configId} version {route.version}">2. {route.configId} · v{route.version}</div>{/if}
						{#each versionViews as v (v.key)}
							{@const sel = !route.event && view.key === v.key}
							<button
								class="item"
								class:sub={!expanded}
								data-selected={sel}
								aria-current={sel ? 'page' : undefined}
								title={v.label}
								onclick={() => go({ configId: route.configId, version: route.version, tab: v.key })}
							>
								{@html icon(v.icon)}<span class="lbl">{expanded ? v.label : v.short}</span>
							</button>
							{#if v.key === 'events' && route.event}
								<button
									class="item event"
									class:sub={!expanded}
									data-selected={true}
									aria-current="page"
									title="Event {route.event}"
									onclick={() => go({ configId: route.configId, version: route.version, event: route.event })}
								>
									{@html icon('event', 16)}<span class="lbl mono">{route.event}</span>
								</button>
							{/if}
						{/each}
					{/if}

					{#if expanded}<div class="area ledgerarea">Ledger</div>{/if}
					<button class="item" data-selected={!!route.ledger} aria-current={route.ledger ? 'page' : undefined} title="Sandbox ledger books" onclick={() => go({ ledger: true })}>
						{@html icon('ledger')}<span class="lbl">{expanded ? 'Sandbox books' : 'Ledger'}</span>
					</button>
				</div>

				<div class="who" title="{me.actor} · {me.roles.map((r) => roleNames[r] ?? r).join(', ')}">
					<span class="avatar" aria-hidden="true">{initials}</span>
					{#if expanded}
						<span class="whotext">
							<b>{me.actor}</b>
							<small>{me.roles.map((r) => roleNames[r] ?? r).join(' · ')}</small>
						</span>
					{/if}
				</div>
			</nav>

			<div class="mainwrap">
				<header class="topbar">
					<button class="iconbtn" aria-label={narrow ? 'Open navigation' : expanded ? 'Narrow the navigation' : 'Widen the navigation'} onclick={toggleNav}
						>{@html icon('menu', 22)}</button
					>
					<span class="areapill" title="Client (tenant): set by the platform when you sign in">{@html icon('client', 16)}<span>Client <b>{me.tenantId}</b></span></span>
					<nav class="crumbs" aria-label="Breadcrumb" bind:this={crumbBar}>
						{#each crumbs as c, i (i)}
							<span class="sep" aria-hidden="true">/</span>
							{#if i < crumbs.length - 1 && c.to}
								<button class="crumb" class:mono={c.mono} onclick={() => go(c.to!)}>{c.label}</button>
							{:else}
								<span class="crumb here" class:mono={c.mono} aria-current="page">{c.label}</span>
							{/if}
						{/each}
					</nav>
					<span class="spacer"></span>
					<button class="iconbtn" aria-label={dark ? 'Use the light theme' : 'Use the dark theme'} title={dark ? 'Light theme' : 'Dark theme'} onclick={toggleTheme}
						>{@html icon(dark ? 'sun' : 'moon')}</button
					>
				</header>
				<main>
					{#if route.ledger}
						{#key tenant}
							<LedgerSandbox {api} {me} book={route.book} {go} />
						{/key}
					{:else if inVersion}
						{#key `${tenant}/${route.configId}/${route.version}`}
							<VersionScope {api} {me} {route} {go} />
						{/key}
					{:else}
						<Home {api} {me} focus={route.configId} {go} />
					{/if}
				</main>
			</div>
		</div>
	{/if}
</div>

<style>
	:host {
		display: block;
	}
	/* Colour, type and spacing tokens. Every studio screen draws with these. */
	.app {
		--gl-bg: #f5f7fa;
		--gl-surface: #ffffff;
		--gl-surface-2: #f5f7fa;
		--gl-border: #e4e7ec;
		--gl-border-strong: #d0d5dd;
		--gl-text: #101828;
		--gl-muted: #475467;
		--gl-subtle: #667085;
		--gl-accent: #224395;
		--gl-accent-hover: #1b367a;
		--gl-accent-soft: #e4e8f2;
		--gl-accent-ink: #ffffff;
		--gl-ok: #067647;
		--gl-ok-soft: #dcfae6;
		--gl-ok-solid: #12b76a;
		--gl-warn: #b54708;
		--gl-warn-soft: #fef0c7;
		--gl-warn-line: #fdb022;
		--gl-bad: #b42318;
		--gl-bad-soft: #fee4e2;
		--gl-bad-line: #f97066;
		--gl-info: #175cd3;
		--gl-info-soft: #d1e9ff;
		--gl-info-line: #84caff;
		--gl-off: #344054;
		--gl-off-soft: #eaecf0;
		--gl-debit: #224395;
		--gl-credit: #6941c6;
		--gl-radius: 12px;
		--gl-radius-sm: 8px;
		--gl-shadow: 0 1px 2px rgb(16 24 40 / 0.05);
		--gl-shadow-pop: 0 12px 16px -4px rgb(16 24 40 / 0.08), 0 4px 6px -2px rgb(16 24 40 / 0.03);
		--gl-font: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
		--gl-mono: 'SFMono-Regular', 'SF Mono', Consolas, 'Liberation Mono', Menlo, monospace;
		--gl-nav: 240px;
		--gl-rail: 72px;
		color: var(--gl-text);
		background: var(--gl-bg);
		font: 14px/1.45 var(--gl-font);
		min-height: 100vh;
		color-scheme: light;
	}
	.app.dark {
		--gl-bg: #0c111d;
		--gl-surface: #161b26;
		--gl-surface-2: #1f242f;
		--gl-border: #2c313c;
		--gl-border-strong: #3f4551;
		--gl-text: #f5f5f6;
		--gl-muted: #cecfd2;
		--gl-subtle: #94969c;
		--gl-accent: #8aa8f0;
		--gl-accent-hover: #a8bff5;
		--gl-accent-soft: #1d2a4d;
		--gl-accent-ink: #0c111d;
		--gl-ok: #47cd89;
		--gl-ok-soft: #053321;
		--gl-ok-solid: #17b26a;
		--gl-warn: #fdb022;
		--gl-warn-soft: #4e1d09;
		--gl-warn-line: #93370d;
		--gl-bad: #f97066;
		--gl-bad-soft: #55160c;
		--gl-bad-line: #912018;
		--gl-info: #53b1fd;
		--gl-info-soft: #102a56;
		--gl-info-line: #1849a9;
		--gl-off: #cecfd2;
		--gl-off-soft: #2c313c;
		--gl-debit: #8aa8f0;
		--gl-credit: #c3a6f5;
		--gl-shadow: 0 1px 2px rgb(0 0 0 / 0.4);
		--gl-shadow-pop: 0 12px 24px rgb(0 0 0 / 0.5);
		color-scheme: dark;
	}

	/* ── shell: side navigation, top bar, page ── */
	.shell {
		display: grid;
		grid-template-columns: var(--gl-nav) minmax(0, 1fr);
		min-height: 100vh;
	}
	.shell.railed {
		grid-template-columns: var(--gl-rail) minmax(0, 1fr);
	}
	.shell.narrow {
		grid-template-columns: minmax(0, 1fr);
	}
	/* --gl-top-offset: the height of anything the host page keeps fixed above the studio (0 in the platform shell). */
	.side {
		position: sticky;
		top: var(--gl-top-offset, 0px);
		height: calc(100vh - var(--gl-top-offset, 0px));
		display: flex;
		flex-direction: column;
		background: var(--gl-bg);
		border-right: 1px solid var(--gl-border);
		overflow: hidden;
		z-index: 20;
	}
	.narrow .side {
		position: fixed;
		left: 0;
		top: 0;
		height: 100vh;
		width: min(var(--gl-nav), 85vw);
		transform: translateX(-100%);
		transition: transform 160ms ease;
		box-shadow: var(--gl-shadow-pop);
	}
	.narrow .side.open {
		transform: none;
	}
	.scrim {
		display: none;
	}
	.scrim.open {
		display: block;
		position: fixed;
		inset: 0;
		background: rgb(16 24 40 / 0.45);
		z-index: 15;
	}
	.brand {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 12px 16px;
		min-height: 56px;
		box-sizing: border-box;
	}
	.railed .brand {
		justify-content: center;
		padding: 12px 8px;
	}
	.brandlink {
		display: flex;
		align-items: center;
		gap: 10px;
		border: 0;
		background: none;
		padding: 0;
		cursor: pointer;
		color: var(--gl-text);
		font: inherit;
		min-width: 0;
	}
	.mark {
		display: grid;
		place-items: center;
		flex: none;
		width: 36px;
		height: 36px;
		border-radius: 8px;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		font-weight: 700;
		font-size: 14px;
		letter-spacing: 0.02em;
	}
	.bname {
		font-weight: 600;
		font-size: 14px;
		line-height: 1.25;
		text-align: left;
	}
	.navscroll {
		flex: 1;
		overflow-y: auto;
		padding-bottom: 8px;
	}
	.area {
		padding: 12px 16px 0;
		font-size: 12px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-text);
	}
	.sect {
		padding: 14px 16px 4px;
		font-size: 12px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-subtle);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sect.current {
		color: var(--gl-accent);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 44px;
		padding: 8px 16px 8px 13px;
		border: 0;
		border-left: 3px solid transparent;
		background: none;
		color: var(--gl-muted);
		font: inherit;
		font-size: 14px;
		text-align: left;
		cursor: pointer;
		box-sizing: border-box;
	}
	.item:hover {
		background: color-mix(in srgb, var(--gl-accent) 6%, transparent);
		color: var(--gl-text);
	}
	.item[data-selected='true'] {
		background: var(--gl-accent-soft);
		color: var(--gl-accent);
		border-left-color: var(--gl-accent);
		font-weight: 600;
	}
	.item :global(.ic) {
		flex: none;
	}
	.item .lbl {
		min-width: 0;
		line-height: 1.25;
		overflow-wrap: anywhere;
	}
	.item.event {
		min-height: 36px;
		padding-left: 41px;
		font-size: 13px;
	}
	/* The 72px rail: icon over a short label. */
	.railed .item {
		flex-direction: column;
		justify-content: center;
		gap: 4px;
		padding: 8px 4px;
		min-height: 56px;
		text-align: center;
		font-size: 11px;
	}
	.railed .item.sub {
		background: var(--gl-surface-2);
	}
	.railed .item.sub[data-selected='true'],
	.railed .item[data-selected='true'] {
		background: var(--gl-accent-soft);
	}
	.railed .item.event {
		padding-left: 4px;
		font-size: 10px;
	}
	.railed .item.event .lbl {
		max-width: 64px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px 16px;
		border-top: 1px solid var(--gl-border);
		min-width: 0;
	}
	.railed .who {
		justify-content: center;
		padding: 12px 4px;
	}
	.avatar {
		display: grid;
		place-items: center;
		flex: none;
		width: 32px;
		height: 32px;
		border-radius: 999px;
		background: var(--gl-off-soft);
		color: var(--gl-muted);
		font-size: 12px;
		font-weight: 600;
	}
	.whotext {
		display: grid;
		min-width: 0;
		line-height: 1.3;
	}
	.whotext b {
		font-size: 13px;
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.whotext small {
		font-size: 12px;
		color: var(--gl-subtle);
	}
	.mainwrap {
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.topbar {
		position: sticky;
		top: var(--gl-top-offset, 0px);
		z-index: 10;
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 4px 20px 4px 16px;
		background: var(--gl-surface);
		border-bottom: 1px solid var(--gl-border);
		box-sizing: border-box;
	}
	.iconbtn {
		display: inline-grid;
		place-items: center;
		flex: none;
		width: 36px;
		height: 36px;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--gl-muted);
		cursor: pointer;
	}
	.iconbtn:hover {
		background: var(--gl-surface-2);
		color: var(--gl-text);
	}
	.areapill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		flex: none;
		font-size: 12px;
		color: var(--gl-text);
		white-space: nowrap;
	}
	.areapill b {
		font-weight: 600;
	}
	.crumbs {
		display: flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
		overflow-x: auto;
		scrollbar-width: none;
		font-size: 12px;
	}
	.sep {
		color: var(--gl-subtle);
	}
	.crumb {
		border: 0;
		background: none;
		padding: 2px 2px;
		font: inherit;
		color: var(--gl-subtle);
		cursor: pointer;
		white-space: nowrap;
		border-radius: 4px;
	}
	.crumb:hover {
		color: var(--gl-accent);
		text-decoration: underline;
	}
	.crumb.here {
		color: var(--gl-text);
		font-weight: 600;
		cursor: default;
	}
	.crumb.mono {
		font-family: var(--gl-mono);
		font-size: 11.5px;
	}
	main {
		scroll-margin-top: calc(var(--gl-top-offset, 0px) + 56px);
		padding: 20px 24px 48px;
		max-width: 1600px;
		width: 100%;
		box-sizing: border-box;
	}
	.state {
		margin: 24px;
		padding: 14px 16px;
		border-radius: var(--gl-radius-sm);
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
	}
	.state.bad {
		background: var(--gl-bad-soft);
		border-color: var(--gl-bad-line);
	}
	.state.warn {
		background: var(--gl-warn-soft);
		border-color: var(--gl-warn-line);
	}
	@media (max-width: 640px) {
		main {
			padding: 16px 16px 40px;
		}
		.topbar {
			padding: 4px 12px 4px 8px;
			gap: 8px;
		}
		.areapill span {
			display: none;
		}
	}

	/* ── page header: eyebrow, title and purpose ── */
	:global(.pagehead) {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px 16px;
		margin: 0 0 16px;
	}
	:global(.pagehead > .titles) {
		min-width: 0;
		flex: 1 1 22rem;
	}
	:global(.eyebrow) {
		display: block;
		font-size: 12px;
		font-weight: 700;
		line-height: 1.4;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-subtle);
		margin-bottom: 2px;
	}
	:global(.pagehead h1) {
		margin: 0;
		font-size: 20px;
		font-weight: 700;
		line-height: 1.35;
		overflow-wrap: anywhere;
	}
	:global(.pagehead .purpose) {
		margin: 4px 0 0;
		font-size: 14px;
		color: var(--gl-muted);
		max-width: 80ch;
	}

	/* ── shared building blocks (used by every studio screen) ── */
	:global(.card) {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		box-shadow: var(--gl-shadow);
		padding: 16px 20px;
		min-width: 0;
	}
	:global(.card + .card),
	:global(.stack > * + *) {
		margin-top: 16px;
	}
	:global(.card h3) {
		margin: 0 0 10px;
		font-size: 16px;
		font-weight: 600;
		line-height: 1.4;
	}
	:global(.card h3 .muted) {
		font-weight: 400;
		font-size: 14px;
	}
	:global(.card h4) {
		margin: 14px 0 6px;
		font-size: 12px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-subtle);
	}
	:global(.grid2) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
		gap: 16px;
		align-items: start;
	}
	:global(.grid3) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 16px;
		align-items: start;
	}
	:global(.grid2 > .card + .card),
	:global(.grid3 > .card + .card) {
		margin-top: 0;
	}
	:global(.muted) {
		color: var(--gl-muted);
	}
	:global(.small) {
		font-size: 12px;
	}
	:global(.mono),
	:global(code) {
		font-family: var(--gl-mono);
	}
	:global(code) {
		font-size: 12px;
		padding: 1px 6px;
		border-radius: 6px;
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
	}
	:global(.btn) {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		min-height: 36px;
		box-sizing: border-box;
		font: inherit;
		font-size: 14px;
		font-weight: 600;
		line-height: 1.2;
		padding: 8px 14px;
		border-radius: var(--gl-radius-sm);
		border: 1px solid var(--gl-accent);
		background: var(--gl-surface);
		color: var(--gl-accent);
		cursor: pointer;
		white-space: nowrap;
		transition:
			background 120ms,
			border-color 120ms,
			color 120ms;
	}
	:global(.btn:hover:not(:disabled)) {
		background: var(--gl-accent-soft);
	}
	:global(.btn.primary) {
		background: var(--gl-accent);
		border-color: var(--gl-accent);
		color: var(--gl-accent-ink);
	}
	:global(.btn.primary:hover:not(:disabled)) {
		background: var(--gl-accent-hover);
		border-color: var(--gl-accent-hover);
	}
	:global(.btn.danger) {
		color: var(--gl-bad);
		border-color: var(--gl-bad-line);
	}
	:global(.btn.danger:hover:not(:disabled)) {
		background: var(--gl-bad-soft);
	}
	:global(.btn.link) {
		min-height: 0;
		border-color: transparent;
		background: none;
		color: var(--gl-link, var(--gl-accent));
		padding: 2px 4px;
	}
	:global(.btn.link:hover:not(:disabled)) {
		background: none;
		text-decoration: underline;
	}
	:global(.btn:disabled) {
		opacity: 0.45;
		cursor: not-allowed;
	}
	:global(.btn:focus-visible),
	:global(button:focus-visible),
	:global(input:focus-visible),
	:global(select:focus-visible),
	:global(textarea:focus-visible) {
		outline: none;
		box-shadow: 0 0 0 4px color-mix(in srgb, var(--gl-accent) 22%, transparent);
	}
	:global(.chip) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 8px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 500;
		line-height: 1.4;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface-2);
		color: var(--gl-text);
		white-space: nowrap;
	}
	:global(.chip.mono) {
		font-size: 11.5px;
	}
	:global(.badge) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 8px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 600;
		line-height: 1.4;
		white-space: nowrap;
	}
	:global(.badge.ok) {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	:global(.badge.bad) {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	:global(.badge.warn) {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
	}
	:global(.badge.info) {
		background: var(--gl-info-soft);
		color: var(--gl-info);
	}
	:global(.badge.off) {
		background: var(--gl-off-soft);
		color: var(--gl-off);
	}
	:global(table.data) {
		width: 100%;
		border-collapse: collapse;
		font-size: 14px;
	}
	:global(table.data th) {
		text-align: left;
		font-size: 12px;
		font-weight: 500;
		color: var(--gl-subtle);
		padding: 10px 12px;
		border-bottom: 1px solid var(--gl-border);
		white-space: nowrap;
	}
	:global(table.data td) {
		padding: 10px 12px;
		border-bottom: 1px solid var(--gl-border);
		vertical-align: top;
	}
	:global(table.data tbody tr:hover td) {
		background: color-mix(in srgb, var(--gl-accent) 4%, transparent);
	}
	:global(table.data tr:last-child td) {
		border-bottom: 0;
	}
	:global(table.data .num) {
		text-align: right;
		font-family: var(--gl-mono);
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	:global(.scroll) {
		overflow-x: auto;
	}
	:global(.notice) {
		padding: 12px 16px;
		border-radius: var(--gl-radius-sm);
		font-size: 14px;
		border: 1px solid transparent;
		color: var(--gl-text);
	}
	:global(.notice.info) {
		background: var(--gl-info-soft);
		border-color: var(--gl-info-line);
	}
	:global(.notice.warn) {
		background: var(--gl-warn-soft);
		border-color: var(--gl-warn-line);
	}
	:global(.notice.bad) {
		background: var(--gl-bad-soft);
		border-color: var(--gl-bad-line);
	}
	:global(.notice.ok) {
		background: var(--gl-ok-soft);
		border-color: color-mix(in srgb, var(--gl-ok-solid) 45%, transparent);
	}
	/* Pill tabs on a framed strip. */
	:global(.tabs) {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 4px;
		margin: 16px 0;
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius-sm);
	}
	:global(.tabs button) {
		display: inline-flex;
		gap: 6px;
		align-items: center;
		font: inherit;
		font-size: 12px;
		font-weight: 600;
		padding: 6px 16px;
		border: 1px solid var(--gl-border);
		border-radius: 6px;
		background: var(--gl-surface);
		color: var(--gl-muted);
		cursor: pointer;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.04);
	}
	:global(.tabs button.active) {
		color: var(--gl-accent-ink);
		background: var(--gl-accent);
		border-color: var(--gl-accent);
	}
	:global(.tabs .count) {
		font-size: 11px;
		padding: 0 6px;
		border-radius: 999px;
		background: var(--gl-surface-2);
		color: var(--gl-muted);
		font-weight: 600;
	}
	:global(.tabs button.active .count) {
		background: rgb(255 255 255 / 0.2);
		color: inherit;
	}
	:global(.kv) {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 6px 16px;
		font-size: 14px;
		margin: 0;
	}
	:global(.kv dt) {
		color: var(--gl-muted);
	}
	:global(.kv dd) {
		margin: 0;
	}
	:global(.side-D) {
		color: var(--gl-debit);
		font-weight: 600;
	}
	:global(.side-C) {
		color: var(--gl-credit);
		font-weight: 600;
	}
	:global(input.field),
	:global(select.field),
	:global(textarea.field) {
		font: inherit;
		font-size: 14px;
		padding: 7px 12px;
		min-height: 38px;
		border-radius: var(--gl-radius-sm);
		border: 1px solid var(--gl-border-strong);
		background: var(--gl-surface);
		color: var(--gl-text);
		min-width: 0;
		box-sizing: border-box;
		transition: border-color 120ms;
	}
	:global(input.field:hover),
	:global(select.field:hover),
	:global(textarea.field:hover) {
		border-color: var(--gl-subtle);
	}
	:global(input.field:focus),
	:global(select.field:focus),
	:global(textarea.field:focus) {
		border-color: var(--gl-accent);
	}
	:global(input.field.mono),
	:global(textarea.field.mono) {
		font-family: var(--gl-mono);
		font-size: 13px;
	}
	:global(label.lbl) {
		display: grid;
		gap: 6px;
		font-size: 13px;
		font-weight: 500;
		color: var(--gl-muted);
	}
	:global(.row) {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
	:global(.spacer) {
		flex: 1;
	}
	:global(.sr) {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
	}
</style>
