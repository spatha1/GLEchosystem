<script lang="ts">
	// Data Workbench mapping is the next backend stage (US-015 proposes mappings through the
	// existing Data Workbench APIs; US-016 reviews, versions and approves them). Until then this
	// page says so plainly and shows exactly what will be sent — it never shows a mapping that
	// does not exist.
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { ManifestView } from '../lib/studio';
	import { digest } from '../lib/format';
	import MappingTable from './MappingTable.svelte';

	let { ctx }: { ctx: Ctx } = $props();
	let mv = $state<ManifestView | null>(null);
	let failure = $state<ApiError | null>(null);

	$effect(() => {
		ctx.tick;
		ctx.api.manifest(ctx.version.configId, ctx.version.version).then(
			(m) => (mv = m),
			(e) => (failure = e)
		);
	});
</script>

<section class="card status">
	<div class="row">
		<span class="badge off">Not built yet</span>
		<h3 class="grow">Data Workbench mapping</h3>
	</div>
	<p>
		The integration with the existing Data Workbench is the next stage of the backlog: <b>US-015</b> sends each event's required fields (below) to the Data
		Workbench, which proposes source tables, columns, joins and transformations with a confidence score; <b>US-016</b> lets a reviewer accept, correct or
		reject each proposal and a different approver approve the mapping version, bound to this configuration version and manifest digest.
	</p>
	<p class="small muted">
		Nothing on this page is a mapping: no proposal, confidence, transformation or approval exists yet, so those columns are empty. Collection (US-017) can
		only run on an approved mapping.
	</p>
</section>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if mv}
	<p class="small muted target">
		Target to register in the Data Workbench: <span class="mono">{mv.manifest.configId}-v{mv.manifest.configVersion}-{digest(mv.manifest.manifestDigest)}</span>
		· only source fields are sent; calculated values are never asked for.
	</p>
	{#each mv.explanation.events as ex, i (ex.eventType)}
		<section class="card">
			<h3><span class="mono">{ex.eventType}</span> <span class="muted">· from {ex.sourceSystems.join(', ')}</span></h3>
			<MappingTable explanation={ex} manifest={mv.manifest.events[i]} />
		</section>
	{/each}
{/if}

<style>
	.status p {
		margin: 8px 0 0;
		max-width: 1000px;
	}
	.grow {
		flex: 1;
		margin: 0;
	}
	.target {
		margin: 14px 0 10px;
	}
</style>
