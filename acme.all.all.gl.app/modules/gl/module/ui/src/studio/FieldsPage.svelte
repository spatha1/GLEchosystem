<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { ManifestView } from '../lib/studio';
	import { digest } from '../lib/format';
	import FieldsTable from './FieldsTable.svelte';

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

	function download() {
		if (!mv) return;
		const blob = new Blob([JSON.stringify(mv.manifest, null, 2)], { type: 'application/json' });
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = `${mv.manifest.configId}-v${mv.manifest.configVersion}.manifest.json`;
		a.click();
		URL.revokeObjectURL(a.href);
	}
</script>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if !mv}
	<p class="muted">Compiling the required-field manifest…</p>
{:else}
	<section class="card head">
		<div class="row">
			<div class="grow">
				<h3>Required-field manifest <span class="muted">(contract C2, US-014)</span></h3>
				<p class="small muted">
					What the sources must supply for this version, compiled from its events, formulas, keys, dates, posting rules and accounts. Calculated values
					are listed with their inputs and never requested. The Data Workbench is asked to find exactly these fields.
				</p>
			</div>
			<button class="btn" onclick={download}>Download manifest (JSON)</button>
		</div>
		<div class="row stats">
			<span class="chip">schema <b class="mono">{mv.manifest.schemaVersion}</b></span>
			<span class="chip">manifest digest <b class="mono">{digest(mv.manifest.manifestDigest)}</b></span>
			<span class="chip">{mv.manifest.events.length} events</span>
			<span class="chip">{mv.manifest.events.reduce((n, e) => n + e.fields.filter((f) => f.kind === 'SOURCE').length, 0)} source fields</span>
			<span class="chip">{mv.manifest.events.reduce((n, e) => n + e.fields.filter((f) => f.kind === 'DERIVED').length, 0)} calculated values</span>
			{#if mv.complete}<span class="badge ok">complete</span>{:else}<span class="badge bad">incomplete: {mv.blockingFindings} blocking finding(s)</span>{/if}
		</div>
	</section>
	{#each mv.explanation.events as ex, i (ex.eventType)}
		<h3 class="evh">
			<button class="btn link mono" onclick={() => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event: ex.eventType })}>{ex.eventType}</button>
			<span class="muted">{ex.description ?? ''}</span>
		</h3>
		<FieldsTable explanation={ex} manifest={mv.manifest.events[i]} />
	{/each}
{/if}

<style>
	.head {
		margin-bottom: 8px;
	}
	.grow {
		flex: 1;
		min-width: 280px;
	}
	.grow p {
		margin: 0;
	}
	.stats {
		margin-top: 10px;
	}
	.evh {
		margin: 22px 0 8px;
		font-size: 15px;
		display: flex;
		gap: 8px;
		align-items: baseline;
	}
	.evh .btn {
		font-size: 15px;
		font-weight: 700;
	}
</style>
