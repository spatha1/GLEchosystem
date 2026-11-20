<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError, type Client } from '../lib/api';
	import { samples } from '../lib/samples';
	import type { ConfigSummary, PostingPlan, Version } from '../lib/types';
	import PlanView from './PlanView.svelte';
	import StatusChip from './StatusChip.svelte';

	let { api, initial = null }: { api: Client; initial?: { configId: string; version: number } | null } = $props();

	let configs = $state<ConfigSummary[]>([]);
	let versions = $state<Version[]>([]);
	let configId = $state('');
	let version = $state(0);
	let sampleId = $state(samples[0].id);
	let text = $state(JSON.stringify(samples[0].event, null, 2));
	let plan = $state<PostingPlan | null>(null);
	let refusal = $state<{ code: string; message: string } | null>(null);
	let running = $state(false);

	const chosen = $derived(versions.find((v) => v.version === version));

	onMount(async () => {
		try {
			configs = await api.configs();
			const id = initial?.configId ?? configs[0]?.configId;
			if (id) await pickConfig(id, initial?.version);
		} catch (e) {
			show(e);
		}
	});

	async function pickConfig(id: string, n?: number) {
		configId = id;
		versions = [...(await api.versions(id))].sort((a, b) => b.version - a.version);
		const approved = versions.find((v) => v.status === 'approved');
		version = n ?? approved?.version ?? versions[0]?.version ?? 0;
		plan = null;
		refusal = null;
	}

	function pickSample(id: string) {
		sampleId = id;
		const s = samples.find((x) => x.id === id);
		if (s) text = JSON.stringify(s.event, null, 2);
		plan = null;
		refusal = null;
	}

	function show(e: unknown) {
		refusal = e instanceof ApiError ? { code: e.code, message: e.message } : { code: 'ERROR', message: String(e) };
	}

	async function run() {
		plan = null;
		refusal = null;
		let event: unknown;
		try {
			event = JSON.parse(text);
		} catch (e) {
			refusal = { code: 'INVALID_JSON', message: `The event is not valid JSON: ${(e as Error).message}` };
			return;
		}
		running = true;
		try {
			plan = await api.plan(configId, version, event);
		} catch (e) {
			show(e);
			// A refused plan (unbalanced, or a leg without a usable account) comes back in the detail.
			const d = e instanceof ApiError ? (e.detail as PostingPlan | undefined) : undefined;
			if (d && Array.isArray(d.lines)) plan = d;
		} finally {
			running = false;
		}
	}
</script>

<div class="controls card">
	<div class="row">
		<label>
			<span>Configuration</span>
			<select value={configId} onchange={(e) => pickConfig((e.currentTarget as HTMLSelectElement).value).catch(show)}>
				{#each configs as c (c.configId)}<option value={c.configId}>{c.configId}</option>{/each}
			</select>
		</label>
		<label>
			<span>Version</span>
			<select bind:value={version}>
				{#each versions as v (v.version)}<option value={v.version}>v{v.version} · {v.status} · from {v.effectiveFrom}</option>{/each}
			</select>
		</label>
		{#if chosen}<div class="status"><StatusChip status={chosen.status} /></div>{/if}
		<label class="grow">
			<span>Sample event (GL-TEST-001)</span>
			<select value={sampleId} onchange={(e) => pickSample((e.currentTarget as HTMLSelectElement).value)}>
				{#each samples as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
			</select>
		</label>
	</div>
	<details>
		<summary>Edit the event</summary>
		<textarea bind:value={text} rows="14" spellcheck="false" aria-label="Event JSON"></textarea>
	</details>
	<div class="run">
		<button class="primary" onclick={run} disabled={running || !configId || !version}>{running ? 'Running…' : 'Preview posting'}</button>
		<span class="muted">Read-only: nothing is posted. Uses the rules, classes, accounts and assignment rules of the chosen version.</span>
	</div>
</div>

{#if refusal}
	<div class="refusal" role="alert">
		<strong>{refusal.code}</strong>
		<span>{refusal.message}</span>
	</div>
{/if}

{#if plan}
	<PlanView {plan} />
{/if}

<style>
	.card {
		background: var(--gl-surface);
		border: 1px solid var(--gl-border);
		border-radius: var(--gl-radius);
		padding: 16px;
		box-shadow: var(--gl-shadow);
		margin-bottom: 16px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: end;
	}
	label {
		display: grid;
		gap: 4px;
		font-size: 12px;
		color: var(--gl-muted);
	}
	label.grow {
		flex: 1 1 280px;
	}
	select,
	textarea {
		font: inherit;
		font-size: 13px;
		color: var(--gl-text);
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
		border-radius: 6px;
		padding: 6px 8px;
	}
	.status {
		padding-bottom: 6px;
	}
	details {
		margin-top: 12px;
	}
	summary {
		cursor: pointer;
		color: var(--gl-accent);
		font-size: 13px;
	}
	textarea {
		width: 100%;
		box-sizing: border-box;
		margin-top: 8px;
		font-family: var(--gl-mono);
		font-size: 12px;
	}
	.run {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: center;
		margin-top: 14px;
	}
	.primary {
		font: inherit;
		font-weight: 600;
		padding: 8px 16px;
		border: 0;
		border-radius: 6px;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		cursor: pointer;
	}
	.primary:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.muted {
		color: var(--gl-muted);
		font-size: 12px;
	}
	.refusal {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		padding: 12px 14px;
		margin-bottom: 16px;
		border-radius: var(--gl-radius);
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	.refusal strong {
		font-family: var(--gl-mono);
	}
</style>
