<script lang="ts">
	import type { Client, ApiError } from '../lib/api';
	import type { ConfigSummary, Me } from '../lib/types';
	import type { Content, VersionFull } from '../lib/studio';
	import type { Route } from '../lib/route';
	import { digest, when } from '../lib/format';
	import StatusChip from '../components/StatusChip.svelte';
	import NewConfigDialog from './NewConfigDialog.svelte';
	import NewDraftDialog from './NewDraftDialog.svelte';

	let { api, me, focus, go }: { api: Client; me: Me; focus?: string; go: (r: Route) => void } = $props();

	let configs = $state<ConfigSummary[] | null>(null);
	let versions = $state<Record<string, VersionFull[]>>({});
	let failure = $state<ApiError | null>(null);
	let creating = $state(false);
	let drafting = $state<{ configId: string; from: number; choose: boolean } | null>(null);
	let busy = $state(false);
	let error = $state('');

	const isEditor = $derived(me.roles.includes('CONFIG_EDITOR'));
	const editorWhy = 'Needs the CONFIG_EDITOR role.';

	$effect(() => {
		api.configs().then(
			async (cs) => {
				configs = cs;
				for (const c of cs) {
					api.versions(c.configId).then((vs) => (versions = { ...versions, [c.configId]: [...vs].sort((a, b) => b.version - a.version) }));
				}
			},
			(e) => (failure = e)
		);
	});

	const live = (vs: VersionFull[] | undefined) => vs?.find((v) => v.status === 'approved');

	async function createConfig(r: { configId: string; title: string; effectiveFrom: string; content: Content; reason: string }) {
		busy = true;
		error = '';
		try {
			const v = await api.createConfig(r.configId, r.title, r.effectiveFrom, r.content, r.reason);
			creating = false;
			go({ configId: v.configId, version: v.version });
		} catch (e) {
			const err = e as ApiError;
			error = `${err.code}: ${err.message}`;
		} finally {
			busy = false;
		}
	}

	async function createDraft(effectiveFrom: string, reason: string, from: number) {
		if (!drafting) return;
		busy = true;
		error = '';
		try {
			const d = await api.newDraft(drafting.configId, from, effectiveFrom, reason);
			drafting = null;
			go({ configId: d.configId, version: d.version });
		} catch (e) {
			const err = e as ApiError;
			error = `${err.code}: ${err.message}`;
		} finally {
			busy = false;
		}
	}
</script>

<section class="pagehead">
	<div class="titles">
		<span class="eyebrow">1 · Configurations · Client {me.tenantId}</span>
		<h1>Configurations</h1>
		<p class="purpose">
			Each configuration is versioned. A version holds the business events, their measures and formulas, keys, dates, posting rules and accounts, and
			moves from draft to validated to approved (by someone other than its editors). Open a version to follow an event from its configuration to its
			journal.
		</p>
	</div>
	<div class="newbox">
		<button class="btn primary" disabled={!isEditor || !configs} title={isEditor ? '' : editorWhy} onclick={() => ((error = ''), (creating = true))}>+ New configuration</button>
		{#if !isEditor}<span class="small muted">{editorWhy}</span>{/if}
	</div>
</section>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if !configs}
	<p class="muted">Loading configurations…</p>
{:else if configs.length === 0}
	<div class="card empty">
		<h3>No configuration yet for {me.tenantId}</h3>
		<p class="muted">Configurations are client-specific: nothing configured for another client is visible here.</p>
		{#if isEditor}<p><button class="btn primary" onclick={() => ((error = ''), (creating = true))}>+ Create the first configuration</button></p>{/if}
	</div>
{:else}
	<div class="stack">
		{#each configs as c (c.configId)}
			{@const vs = versions[c.configId]}
			{@const current = live(vs)}
			<article class="card config" class:focus={focus === c.configId}>
				<header>
					<div>
						<div class="cid mono">{c.configId}</div>
						<div class="title">{c.title || 'Untitled configuration'}</div>
						<div class="muted small">Created by {c.createdBy} · {when(c.createdAt)}</div>
					</div>
					<div class="row">
						{#if current}
							<span class="badge ok">In force: v{current.version}</span>
						{:else}
							<span class="badge warn">No approved version</span>
						{/if}
						<span class="chip">{c.latestVersion} version{c.latestVersion === 1 ? '' : 's'}</span>
						{#if vs?.length}
							<button
								class="btn"
								disabled={!isEditor}
								title={isEditor ? '' : editorWhy}
								onclick={() => ((error = ''), (drafting = { configId: c.configId, from: 0, choose: true }))}>+ New version</button
							>
						{/if}
					</div>
				</header>
				{#if !vs}
					<p class="muted small">Loading versions…</p>
				{:else}
					<div class="scroll">
						<table class="data versions">
							<thead>
								<tr>
									<th>Version</th>
									<th>Status</th>
									<th>Effective</th>
									<th>Based on</th>
									<th>Changed by</th>
									<th>Approved by</th>
									<th>Digest</th>
									<th></th>
								</tr>
							</thead>
							<tbody>
								{#each vs as v (v.version)}
									<tr>
										<td><button class="btn link mono" onclick={() => go({ configId: c.configId, version: v.version })}>v{v.version}</button></td>
										<td><StatusChip status={v.status} /></td>
										<td class="nowrap">{v.effectiveFrom} → {v.effectiveTo ?? 'open'}</td>
										<td>{v.basedOnVersion ? `v${v.basedOnVersion}` : '—'}</td>
										<td class="small">{v.editors.join(', ')}</td>
										<td class="small">{v.approvedBy ?? '—'}</td>
										<td class="mono small">{digest(v.contentDigest)}</td>
										<td class="right nowrap">
											{#if isEditor}
												<button class="btn" title="A new draft copied from v{v.version}" onclick={() => ((error = ''), (drafting = { configId: c.configId, from: v.version, choose: false }))}
													>New draft</button
												>
											{/if}
											<button class="btn" onclick={() => go({ configId: c.configId, version: v.version })}>Open</button>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</article>
		{/each}
	</div>
{/if}

{#if creating && configs}
	<NewConfigDialog
		{configs}
		{versions}
		load={async (id, n) => ((await api.version(id, n)).content ?? {}) as Content}
		{busy}
		{error}
		onsubmit={createConfig}
		oncancel={() => (creating = false)}
	/>
{/if}
{#if drafting}
	<NewDraftDialog
		from={drafting.from}
		choices={drafting.choose ? (versions[drafting.configId] ?? []).map((v) => ({ version: v.version, status: v.status })) : []}
		{busy}
		{error}
		onsubmit={createDraft}
		oncancel={() => (drafting = null)}
	/>
{/if}

<style>
	.newbox {
		display: grid;
		gap: 4px;
		justify-items: end;
	}
	.config {
		padding: 0;
		overflow: hidden;
	}
	.config header {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: flex-start;
		gap: 10px;
		padding: 16px 20px 14px;
		border-bottom: 1px solid var(--gl-border);
	}
	.config .scroll {
		padding: 0 8px 4px;
	}
	.config.focus {
		border-color: var(--gl-accent);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--gl-accent) 15%, transparent);
	}
	.cid {
		font-weight: 600;
		font-size: 13px;
		color: var(--gl-accent);
	}
	.title {
		font-weight: 600;
		font-size: 16px;
		margin: 2px 0;
	}
	.nowrap {
		white-space: nowrap;
	}
	.right {
		text-align: right;
	}
	.right .btn + .btn {
		margin-left: 8px;
	}
	.empty p {
		margin: 0 0 8px;
	}
</style>
