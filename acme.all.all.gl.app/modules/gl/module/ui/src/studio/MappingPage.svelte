<script lang="ts">
	// Data Workbench mapping is the next backend stage (US-015 proposes mappings through the
	// existing Data Workbench APIs; US-016 reviews, versions and approves them). Until then this
	// page says so plainly and shows exactly what will be sent — it never shows a mapping that
	// does not exist.
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { ManifestView } from '../lib/studio';
	import { digest } from '../lib/format';

	let { ctx }: { ctx: Ctx } = $props();
	let mv = $state<ManifestView | null>(null);
	let failure = $state<ApiError | null>(null);
	let evidence = $state('');

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
			<div class="scroll">
				<table class="data">
					<thead>
						<tr>
							<th>Required field</th>
							<th>Type</th>
							<th>Proposed source table / column</th>
							<th>Confidence</th>
							<th>Transformation</th>
							<th>Approval</th>
							<th>Mapping version</th>
							<th>Evidence</th>
						</tr>
					</thead>
					<tbody>
						{#each ex.fields.filter((f) => f.kind === 'SOURCE') as f (f.node)}
							{@const m = mv.manifest.events[i].fields.find((x) => x.fieldCode === f.fieldCode && (x.listCode ?? '') === (f.listCode ?? ''))}
							<tr>
								<td>
									<span class="mono"><b>{f.listCode ? `${f.listCode}[ ].` : ''}{f.fieldCode}</b></span>
									{#if f.mandatory}<span class="badge bad">mandatory</span>{/if}
								</td>
								<td class="small">{m?.dataType.toLowerCase()}{m?.cardinality === 'LIST' ? ' (1:N)' : ''}</td>
								<td class="muted">— no proposal</td>
								<td class="muted">—</td>
								<td class="small">
									{#each f.constraints?.filter((c) => c.includes(' sends ')) ?? [] as c (c)}<div>configured normalisation: {c}</div>{:else}<span class="muted">—</span>{/each}
								</td>
								<td><span class="badge off">not mapped</span></td>
								<td class="muted">—</td>
								<td><button class="btn" onclick={() => (evidence = `${ex.eventType}.${f.fieldCode}`)}>Evidence</button></td>
							</tr>
							{#if evidence === `${ex.eventType}.${f.fieldCode}`}
								<tr class="ev">
									<td colspan="8">
										<p class="small">
											No evidence: the Data Workbench has not been asked yet. When US-015 is built, this shows the Data Workbench's candidate columns, sample
											values, join path, method and score exactly as received; the request will ask for <span class="mono">{f.path}</span> ({f.label}).
										</p>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
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
	.ev td {
		background: var(--gl-surface-2);
	}
	.ev p {
		margin: 0;
	}
</style>
