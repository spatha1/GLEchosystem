<svelte:options customElement={{ tag: 'gl-app', props: { tenant: { type: 'String', attribute: 'tenant' } } }} />

<script lang="ts">
	import { client, type ApiError } from './lib/api';
	import type { Me } from './lib/types';
	import Configurations from './components/Configurations.svelte';
	import PostingPreview from './components/PostingPreview.svelte';

	let { tenant = '' }: { tenant?: string } = $props();

	const roleNames: Record<string, string> = {
		GL_ADMIN: 'Administrator',
		CONFIG_EDITOR: 'Editor',
		CONFIG_APPROVER: 'Approver',
		OPERATOR: 'Operator',
		VIEWER: 'Viewer'
	};

	let me = $state<Me | null>(null);
	let failure = $state<ApiError | null>(null);
	let tab = $state<'configs' | 'preview'>('configs');
	let target = $state<{ configId: string; version: number } | null>(null);
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

	function preview(configId: string, version: number) {
		target = { configId, version };
		tab = 'preview';
	}
</script>

<div class="app">
	<header class="top">
		<div class="brand">
			<span class="mark" aria-hidden="true">GL</span>
			<div>
				<div class="name">General Ledger</div>
				<div class="sub">Configuration and posting</div>
			</div>
		</div>
		{#if me}
			<div class="who">
				<span class="tenant" title="Tenant">{me.tenantId}</span>
				<span class="actor">{me.actor}</span>
				{#each me.roles as r (r)}
					<span class="role">{roleNames[r] ?? r}</span>
				{/each}
			</div>
		{/if}
	</header>

	{#if !tenant}
		<div class="state">No tenant is selected.</div>
	{:else if failure}
		<div class="state bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
	{:else if !me}
		<div class="state">Loading…</div>
	{:else if me.roles.length === 0}
		<div class="state warn" role="alert">You have no role in tenant {me.tenantId}. Ask a GL administrator for access.</div>
	{:else}
		<div class="tabs" role="tablist" aria-label="GL screens">
			<button role="tab" aria-selected={tab === 'configs'} class:active={tab === 'configs'} onclick={() => (tab = 'configs')}>Configurations</button>
			<button role="tab" aria-selected={tab === 'preview'} class:active={tab === 'preview'} onclick={() => (tab = 'preview')}>Posting preview</button>
		</div>
		<main>
			{#if tab === 'configs'}
				<Configurations {api} {me} onpreview={preview} />
			{:else}
				<PostingPreview {api} initial={target} />
			{/if}
		</main>
	{/if}
</div>

<style>
	:host {
		display: block;
		--gl-bg: #f3f5f8;
		--gl-surface: #ffffff;
		--gl-surface-2: #f8fafc;
		--gl-border: #dde3ea;
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
		padding: 14px 24px;
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
		background: var(--gl-accent);
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
	.tenant {
		font-family: var(--gl-mono);
		font-weight: 600;
		padding: 2px 8px;
		border-radius: 999px;
		background: var(--gl-accent-soft);
		color: var(--gl-accent);
	}
	.actor {
		color: var(--gl-muted);
		margin-right: 4px;
	}
	.role {
		padding: 2px 8px;
		border-radius: 999px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface-2);
	}
	.tabs {
		display: flex;
		gap: 4px;
		padding: 0 24px;
		background: var(--gl-surface);
		border-bottom: 1px solid var(--gl-border);
	}
	.tabs button {
		font: inherit;
		padding: 10px 14px;
		border: 0;
		border-bottom: 2px solid transparent;
		background: none;
		color: var(--gl-muted);
		cursor: pointer;
	}
	.tabs button.active {
		color: var(--gl-text);
		border-bottom-color: var(--gl-accent);
		font-weight: 600;
	}
	.tabs button:focus-visible {
		outline: 2px solid var(--gl-accent);
		outline-offset: -2px;
	}
	main {
		padding: 20px 24px 32px;
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
		main {
			padding-left: 16px;
			padding-right: 16px;
		}
		.tabs {
			padding: 0 8px;
		}
	}
</style>
