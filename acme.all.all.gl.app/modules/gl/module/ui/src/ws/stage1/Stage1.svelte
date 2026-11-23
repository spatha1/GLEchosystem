<script lang="ts">
	// Stage 1 — the chart of accounts, as five steps in order: import, map classes, assignment
	// rules, review, submit. It works on the client's configuration: the latest version, edited
	// when it is a draft; an approved version is read-only until a new draft is started.
	import type { Content, VersionFull } from '../../lib/studio';
	import type { Me } from '../../lib/types';
	import type { ClassLibrary, WsClient } from '../api';
	import type { LoadedFile } from '../files';
	import { STAGE1_STEPS, type WsRoute } from '../route';
	import type { S1 } from './ctx';
	import { accounts, sourceClasses, rules } from './model';
	import Step1Import from './Step1Import.svelte';
	import Step2Classes from './Step2Classes.svelte';
	import Step3Assignments from './Step3Assignments.svelte';
	import Step4Review from './Step4Review.svelte';
	import Step5Submit from './Step5Submit.svelte';

	let { api, me, clientLabel, step, go }: { api: WsClient; me: Me; clientLabel: string; step: number; go: (r: WsRoute) => void } = $props();

	let configId = $state('');
	let version = $state<VersionFull | null>(null);
	let versions = $state<VersionFull[]>([]);
	let library = $state<ClassLibrary | null>(null);
	let systems = $state<string[]>([]);
	let files = $state<LoadedFile[]>([]);
	let error = $state('');
	let loading = $state(true);
	let drafting = $state(false);
	let draftFrom = $state('');
	let draftWhy = $state('');

	const isEditor = $derived(me.roles.includes('CONFIG_EDITOR'));
	const isApprover = $derived(me.roles.includes('CONFIG_APPROVER'));

	async function load() {
		error = '';
		try {
			const [profile, cfgs, lib] = await Promise.all([api.getClient().catch(() => null), api.configs(), api.library()]);
			library = lib;
			systems = profile?.profile?.sourceSystems ?? [];
			configId = profile?.configId && cfgs.some((c) => c.configId === profile.configId) ? profile.configId : (cfgs[0]?.configId ?? '');
			if (!configId) {
				version = null;
				return;
			}
			versions = await api.versions(configId);
			const last = versions.at(-1)!;
			version = await api.version(configId, last.version);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		api;
		load();
	});

	const content = $derived((version?.content ?? {}) as Content);
	const canEdit = $derived(!!version && (version.status === 'draft' || version.status === 'validated') && isEditor);

	async function save(next: Content, reason: string) {
		if (!version) return;
		await api.saveDraft(version.configId, version.version, version.contentDigest, next, reason);
		version = await api.version(version.configId, version.version);
	}

	async function startDraft() {
		if (!version) return;
		error = '';
		try {
			const d = await api.newDraft(version.configId, version.version, draftFrom, draftWhy);
			drafting = false;
			versions = await api.versions(configId);
			version = await api.version(configId, d.version);
		} catch (e) {
			error = (e as Error).message;
		}
	}
	function openDraft() {
		const taken = new Set(versions.filter((v) => v.status === 'approved').map((v) => v.effectiveFrom));
		const d = new Date(Math.max(Date.now(), Date.parse(version?.effectiveFrom ?? '') || 0));
		while (taken.has(d.toISOString().slice(0, 10))) d.setUTCDate(d.getUTCDate() + 1);
		draftFrom = d.toISOString().slice(0, 10);
		draftWhy = '';
		drafting = true;
	}

	const ctx = $derived<S1 | null>(
		version && library
			? {
					api,
					me,
					version,
					content,
					canEdit,
					isEditor,
					isApprover,
					library,
					systems,
					files,
					addFiles: (f) => (files = [...files.filter((x) => !f.some((y) => y.name === x.name)), ...f]),
					save,
					reload: load,
					step: (n) => go({ page: 'stage1', client: api.tenant, step: n })
				}
			: null
	);

	// how far each step is, from the content
	const progress = $derived.by(() => {
		const sc = sourceClasses(content);
		const mapped = sc.filter((s) => s.status === 'NOT_USED' || (s.mappings ?? []).length).length;
		return [
			accounts(content).length ? 'done' : 'todo',
			sc.length && mapped === sc.length ? 'done' : sc.length ? 'doing' : 'todo',
			rules(content).length ? 'done' : 'todo',
			version?.status === 'validated' || version?.status === 'approved' ? 'done' : 'todo',
			version?.status === 'approved' ? 'done' : version?.status === 'validated' ? 'doing' : 'todo'
		];
	});
	const statusLabel: Record<string, string> = { draft: 'Draft', validated: 'Submitted for review', approved: 'Approved', retired: 'Retired' };
</script>

<div class="s1head">
	<div>
		<h1 class="ws-h1">Stage 1 · Chart of accounts</h1>
		<p class="ws-lead">{clientLabel}: the client’s accounts, its accounting classes mapped to the standard library, and the rules that choose an account for each class.</p>
	</div>
	{#if version}
		<div class="vbar card">
			<span class="mono">{version.configId}</span>
			<span class="pill">v{version.version}</span>
			<span class="pill {version.status === 'approved' ? 'ok' : version.status === 'validated' ? 'info' : version.status === 'draft' ? 'warn' : ''}">{statusLabel[version.status]}</span>
			<span class="small muted">from {version.effectiveFrom}</span>
			{#if version.status === 'approved' && isEditor}<button class="btn small" onclick={openDraft}>New draft…</button>{/if}
		</div>
	{/if}
</div>

<ol class="stepper" aria-label="Stage 1 steps">
	{#each STAGE1_STEPS as s, i (s.n)}
		<li class:on={step === s.n} class:done={progress[i] === 'done'} class:doing={progress[i] === 'doing'}>
			<button onclick={() => go({ page: 'stage1', client: api.tenant, step: s.n })} aria-current={step === s.n ? 'step' : undefined}>
				<span class="dot">{progress[i] === 'done' ? '✓' : s.n}</span>
				<span class="txt"><b>{s.title}</b><small>{s.hint}</small></span>
			</button>
		</li>
	{/each}
</ol>

{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
{#if drafting}
	<div class="card draft">
		<h2 class="sec">New draft from v{version?.version}</h2>
		<p class="small muted">Approved versions never change. The new draft copies v{version?.version}; it must start on a date no other approved version starts on.</p>
		<div class="row">
			<label class="lbl">Applies from <input class="field" type="date" bind:value={draftFrom} /></label>
			<label class="lbl grow">Reason <input class="field" bind:value={draftWhy} placeholder="What will change" /></label>
			<button class="btn primary" disabled={!draftWhy.trim() || !draftFrom} onclick={startDraft}>Create draft</button>
			<button class="btn ghost" onclick={() => (drafting = false)}>Cancel</button>
		</div>
	</div>
{/if}

{#if loading}
	<p class="muted">Loading…</p>
{:else if !configId}
	<div class="card">
		<h2 class="sec">No configuration yet</h2>
		<p>An administrator onboards the client, which creates its empty draft configuration.</p>
	</div>
{:else if ctx}
	{#if !canEdit && version?.status === 'draft'}<p class="notice small">You can read this draft; editing needs the business analyst role.</p>{/if}
	{#if version?.status === 'approved'}<p class="notice ok small">v{version.version} is approved and can no longer change. Start a new draft to change it.</p>{/if}
	{#key step}
		{#if step === 1}<Step1Import {ctx} />{:else if step === 2}<Step2Classes {ctx} />{:else if step === 3}<Step3Assignments {ctx} />{:else if step === 4}<Step4Review {ctx} />{:else}<Step5Submit {ctx} />{/if}
	{/key}
	<div class="nav row">
		{#if step > 1}<button class="btn" onclick={() => go({ page: 'stage1', client: api.tenant, step: step - 1 })}>← Back</button>{/if}
		<span class="grow"></span>
		{#if step < 5}<button class="btn primary" onclick={() => go({ page: 'stage1', client: api.tenant, step: step + 1 })}>Next: {STAGE1_STEPS[step].title} →</button>{/if}
	</div>
{/if}

<style>
	.s1head {
		display: flex;
		gap: 16px;
		align-items: flex-start;
		justify-content: space-between;
		flex-wrap: wrap;
	}
	.vbar {
		display: flex;
		gap: 8px;
		align-items: center;
		padding: 10px 14px;
	}
	.stepper {
		list-style: none;
		padding: 0;
		margin: 4px 0 18px;
		display: grid;
		grid-template-columns: repeat(5, minmax(0, 1fr));
		gap: 8px;
	}
	.stepper li button {
		width: 100%;
		text-align: left;
		font: inherit;
		background: var(--card);
		border: 1px solid var(--line);
		border-radius: 12px;
		padding: 10px 12px;
		display: flex;
		gap: 10px;
		align-items: center;
		cursor: pointer;
		color: var(--text);
		height: 100%;
	}
	.stepper li.on button {
		border-color: var(--accent);
		box-shadow: 0 0 0 2px var(--accent-soft);
	}
	.dot {
		flex: none;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-size: 12px;
		font-weight: 700;
		background: var(--bg);
		border: 1px solid var(--line);
		color: var(--muted);
	}
	.stepper li.on .dot {
		background: var(--accent);
		border-color: var(--accent);
		color: #fff;
	}
	.stepper li.done .dot {
		background: var(--ok-soft);
		border-color: transparent;
		color: var(--ok);
	}
	.stepper li.doing .dot {
		background: var(--warn-soft);
		border-color: transparent;
		color: var(--warn);
	}
	.txt {
		display: grid;
		line-height: 1.25;
	}
	.txt small {
		color: var(--muted);
		font-size: 11.5px;
	}
	.draft {
		margin-bottom: 14px;
	}
	.grow {
		flex: 1;
	}
	.nav {
		margin-top: 18px;
	}
	@media (max-width: 1000px) {
		.stepper {
			grid-template-columns: 1fr 1fr;
		}
	}
</style>
