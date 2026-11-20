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
	import Home from './studio/Home.svelte';
	import VersionScope from './studio/VersionScope.svelte';

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
		if (hashRouting) {
			const h = toHash(r);
			if (location.hash !== h) location.hash = h;
			else route = r;
		} else {
			route = r;
		}
		queueMicrotask(() => (document.querySelector('gl-app')?.shadowRoot?.querySelector('main') as HTMLElement | null)?.scrollIntoView?.({ block: 'start' }));
	}

	const crumbs = $derived.by(() => {
		const out: { label: string; sub?: string; to: Route }[] = [{ label: me?.tenantId ?? tenant, sub: 'Client', to: {} }];
		if (route.configId) out.push({ label: route.configId, sub: 'Configuration', to: { configId: route.configId } });
		if (route.configId && route.version) out.push({ label: `v${route.version}`, sub: 'Version', to: { configId: route.configId, version: route.version } });
		if (route.event) out.push({ label: route.event, sub: 'Event', to: { configId: route.configId, version: route.version, event: route.event } });
		if (route.rule)
			out.push({ label: route.rule === '+new' ? 'New rule' : route.rule, sub: 'Posting rule', to: { ...route, edit: false, scenario: undefined } });
		return out;
	});
</script>

<div class="app">
	<header class="top">
		<div class="brand">
			<span class="mark" aria-hidden="true">GL</span>
			<div>
				<div class="name">GL Configuration Studio</div>
				<div class="sub">Events · rules · fields · simulation · journals</div>
			</div>
		</div>
		{#if me}
			<div class="who">
				<span class="client" title="Client (tenant)">Client <b>{me.tenantId}</b></span>
				<span class="actor">{me.actor}</span>
				{#each me.roles as r (r)}
					<span class="role">{roleNames[r] ?? r}</span>
				{/each}
			</div>
		{/if}
	</header>

	{#if !tenant}
		<div class="state">No client is selected.</div>
	{:else if failure}
		<div class="state bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
	{:else if !me}
		<div class="state">Loading…</div>
	{:else if me.roles.length === 0}
		<div class="state warn" role="alert">You have no role for client {me.tenantId}. Ask a GL administrator for access.</div>
	{:else}
		<nav class="crumbs" aria-label="Breadcrumb">
			{#each crumbs as c, i (i)}
				{#if i > 0}<span class="sep" aria-hidden="true">›</span>{/if}
				{#if i < crumbs.length - 1}
					<button class="crumb" onclick={() => go(c.to)}><span class="k">{c.sub}</span>{c.label}</button>
				{:else}
					<span class="crumb here" aria-current="page"><span class="k">{c.sub}</span>{c.label}</span>
				{/if}
			{/each}
		</nav>
		<main>
			{#if route.configId && route.version}
				{#key `${tenant}/${route.configId}/${route.version}`}
					<VersionScope {api} {me} {route} {go} />
				{/key}
			{:else}
				<Home {api} {me} focus={route.configId} {go} />
			{/if}
		</main>
	{/if}
</div>

<style>
	:host {
		display: block;
		--gl-bg: #f3f5f8;
		--gl-surface: #ffffff;
		--gl-surface-2: #f7f9fb;
		--gl-border: #dde3ea;
		--gl-border-strong: #c5ced9;
		--gl-text: #16202b;
		--gl-muted: #5b6878;
		--gl-accent: #1d5c8c;
		--gl-accent-soft: #e4eef7;
		--gl-accent-ink: #ffffff;
		--gl-ok: #1b7348;
		--gl-ok-soft: #e1f3e9;
		--gl-warn: #8f5a00;
		--gl-warn-soft: #fcf0d9;
		--gl-bad: #b42318;
		--gl-bad-soft: #fde7e5;
		--gl-info: #27529c;
		--gl-info-soft: #e6edf9;
		--gl-off: #6b7280;
		--gl-off-soft: #eceff3;
		--gl-debit: #1d5c8c;
		--gl-credit: #7a3e9d;
		--gl-radius: 8px;
		--gl-shadow: 0 1px 2px rgba(16, 24, 40, 0.06), 0 1px 3px rgba(16, 24, 40, 0.08);
		--gl-mono: ui-monospace, 'Cascadia Mono', 'SFMono-Regular', Menlo, Consolas, monospace;
		color: var(--gl-text);
		font: 14px/1.45 system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', sans-serif;
	}
	@media (prefers-color-scheme: dark) {
		:host {
			--gl-bg: #0f141a;
			--gl-surface: #161d25;
			--gl-surface-2: #1b232d;
			--gl-border: #2a3542;
			--gl-border-strong: #3a4757;
			--gl-text: #e3e8ef;
			--gl-muted: #94a2b3;
			--gl-accent: #5aa6e0;
			--gl-accent-soft: #1b3044;
			--gl-accent-ink: #08131d;
			--gl-ok: #57c48c;
			--gl-ok-soft: #13301f;
			--gl-warn: #e6ad4e;
			--gl-warn-soft: #36280f;
			--gl-bad: #f2827a;
			--gl-bad-soft: #3a1917;
			--gl-info: #8fb4ee;
			--gl-info-soft: #18263d;
			--gl-off: #9aa3ae;
			--gl-off-soft: #232b35;
			--gl-debit: #6db3ea;
			--gl-credit: #c99be6;
			--gl-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
		}
	}
	.app {
		background: var(--gl-bg);
		min-height: 100%;
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: center;
		justify-content: space-between;
		padding: 12px 24px;
		background: var(--gl-surface);
		border-bottom: 1px solid var(--gl-border);
	}
	.brand {
		display: flex;
		gap: 12px;
		align-items: center;
	}
	.mark {
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: 9px;
		background: linear-gradient(135deg, var(--gl-accent), #2f7fb8);
		color: var(--gl-accent-ink);
		font-weight: 700;
		letter-spacing: 0.5px;
	}
	.name {
		font-weight: 650;
		font-size: 16px;
	}
	.sub {
		color: var(--gl-muted);
		font-size: 12px;
	}
	.who {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
		font-size: 12px;
	}
	.client {
		padding: 3px 10px;
		border-radius: 999px;
		background: var(--gl-accent-soft);
		color: var(--gl-accent);
	}
	.client b {
		font-family: var(--gl-mono);
	}
	.actor {
		color: var(--gl-muted);
		margin: 0 2px 0 6px;
	}
	.role {
		padding: 2px 8px;
		border-radius: 999px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface-2);
	}
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
		padding: 8px 24px;
		background: var(--gl-surface);
		border-bottom: 1px solid var(--gl-border);
		font-size: 13px;
	}
	.crumb {
		display: inline-flex;
		flex-direction: column;
		align-items: flex-start;
		font: inherit;
		font-family: var(--gl-mono);
		font-weight: 600;
		padding: 2px 8px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--gl-accent);
		cursor: pointer;
		line-height: 1.25;
	}
	.crumb:hover {
		background: var(--gl-accent-soft);
	}
	.crumb.here {
		color: var(--gl-text);
		cursor: default;
	}
	.crumb .k {
		font-family: system-ui, sans-serif;
		font-weight: 500;
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--gl-muted);
	}
	.sep {
		color: var(--gl-muted);
	}
	main {
		padding: 20px 24px 40px;
		max-width: 1440px;
	}
	.state {
		margin: 24px;
		padding: 14px 16px;
		border-radius: var(--gl-radius);
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
	}
	.state.bad {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
		border-color: transparent;
	}
	.state.warn {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
		border-color: transparent;
	}
	@media (max-width: 640px) {
		.top,
		main,
		.crumbs {
			padding-left: 16px;
			padding-right: 16px;
		}
	}

	/* ── shared building blocks (used by every studio screen) ── */
	:global(.card) {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		box-shadow: var(--gl-shadow);
		padding: 16px;
		min-width: 0;
	}
	:global(.card + .card),
	:global(.stack > * + *) {
		margin-top: 14px;
	}
	:global(.card h3) {
		margin: 0 0 10px;
		font-size: 14px;
		font-weight: 650;
	}
	:global(.card h3 .muted) {
		font-weight: 500;
	}
	:global(.card h4) {
		margin: 12px 0 6px;
		font-size: 12px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
	}
	:global(.grid2) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(340px, 1fr));
		gap: 14px;
		align-items: start;
	}
	:global(.grid3) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 14px;
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
		padding: 1px 5px;
		border-radius: 4px;
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
	}
	:global(.btn) {
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		padding: 6px 12px;
		border-radius: 6px;
		border: 1px solid var(--gl-border-strong);
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
		white-space: nowrap;
	}
	:global(.btn:hover:not(:disabled)) {
		border-color: var(--gl-accent);
		color: var(--gl-accent);
	}
	:global(.btn.primary) {
		background: var(--gl-accent);
		border-color: var(--gl-accent);
		color: var(--gl-accent-ink);
	}
	:global(.btn.primary:hover:not(:disabled)) {
		filter: brightness(1.08);
		color: var(--gl-accent-ink);
	}
	:global(.btn.danger) {
		color: var(--gl-bad);
	}
	:global(.btn.link) {
		border-color: transparent;
		background: none;
		color: var(--gl-accent);
		padding: 2px 4px;
	}
	:global(.btn:disabled) {
		opacity: 0.5;
		cursor: not-allowed;
	}
	:global(.btn:focus-visible),
	:global(button:focus-visible),
	:global(input:focus-visible),
	:global(select:focus-visible),
	:global(textarea:focus-visible) {
		outline: 2px solid var(--gl-accent);
		outline-offset: 1px;
	}
	:global(.chip) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 1px 8px;
		border-radius: 999px;
		font-size: 12px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface-2);
		white-space: nowrap;
	}
	:global(.chip.mono) {
		font-size: 11.5px;
	}
	:global(.badge) {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 1px 8px;
		border-radius: 999px;
		font-size: 11.5px;
		font-weight: 650;
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
		font-size: 13px;
	}
	:global(table.data th) {
		text-align: left;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
		padding: 6px 8px;
		border-bottom: 1px solid var(--gl-border);
		white-space: nowrap;
	}
	:global(table.data td) {
		padding: 7px 8px;
		border-bottom: 1px solid var(--gl-border);
		vertical-align: top;
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
		padding: 10px 12px;
		border-radius: var(--gl-radius);
		font-size: 13px;
		border: 1px solid transparent;
	}
	:global(.notice.info) {
		background: var(--gl-info-soft);
		color: var(--gl-info);
	}
	:global(.notice.warn) {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
	}
	:global(.notice.bad) {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	:global(.notice.ok) {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	:global(.tabs) {
		display: flex;
		flex-wrap: wrap;
		gap: 2px;
		border-bottom: 1px solid var(--gl-border);
		margin: 16px 0;
	}
	:global(.tabs button) {
		font: inherit;
		font-size: 13.5px;
		padding: 9px 12px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--gl-muted);
		cursor: pointer;
		display: inline-flex;
		gap: 6px;
		align-items: center;
	}
	:global(.tabs button.active) {
		color: var(--gl-text);
		border-bottom-color: var(--gl-accent);
		font-weight: 600;
	}
	:global(.tabs .count) {
		font-size: 11px;
		padding: 0 6px;
		border-radius: 999px;
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
		color: var(--gl-muted);
		font-weight: 600;
	}
	:global(.kv) {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 4px 14px;
		font-size: 13px;
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
		font-weight: 650;
	}
	:global(.side-C) {
		color: var(--gl-credit);
		font-weight: 650;
	}
	:global(input.field),
	:global(select.field),
	:global(textarea.field) {
		font: inherit;
		font-size: 13px;
		padding: 5px 8px;
		border-radius: 6px;
		border: 1px solid var(--gl-border-strong);
		background: var(--gl-surface);
		color: var(--gl-text);
		min-width: 0;
		box-sizing: border-box;
	}
	:global(input.field.mono),
	:global(textarea.field.mono) {
		font-family: var(--gl-mono);
		font-size: 12.5px;
	}
	:global(label.lbl) {
		display: grid;
		gap: 3px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
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
