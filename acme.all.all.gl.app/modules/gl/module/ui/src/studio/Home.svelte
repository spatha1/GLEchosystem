<script lang="ts">
	import type { Client, ApiError } from '../lib/api';
	import type { ConfigSummary, Me } from '../lib/types';
	import type { VersionFull } from '../lib/studio';
	import type { Route } from '../lib/route';
	import { digest, when } from '../lib/format';
	import StatusChip from '../components/StatusChip.svelte';

	let { api, me, focus, go }: { api: Client; me: Me; focus?: string; go: (r: Route) => void } = $props();

	let configs = $state<ConfigSummary[] | null>(null);
	let versions = $state<Record<string, VersionFull[]>>({});
	let failure = $state<ApiError | null>(null);

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
</script>

<section class="intro">
	<div>
		<h2>Configurations of client <span class="mono">{me.tenantId}</span></h2>
		<p class="muted">
			Each configuration is versioned. A version holds the business events, their measures and formulas, keys, dates, posting rules and accounts, and
			moves from draft to validated to approved (by someone other than its editors). Open a version to follow an event from its configuration to its
			journal.
		</p>
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
										<td class="right"><button class="btn" onclick={() => go({ configId: c.configId, version: v.version })}>Open</button></td>
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

<style>
	.intro h2 {
		margin: 0 0 4px;
		font-size: 18px;
	}
	.intro p {
		margin: 0 0 16px;
		max-width: 900px;
	}
	.config header {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 10px;
		margin-bottom: 10px;
	}
	.config.focus {
		border-color: var(--gl-accent);
	}
	.cid {
		font-weight: 700;
		font-size: 15px;
	}
	.title {
		font-weight: 550;
	}
	.nowrap {
		white-space: nowrap;
	}
	.right {
		text-align: right;
	}
	.empty p {
		margin: 0;
	}
</style>
