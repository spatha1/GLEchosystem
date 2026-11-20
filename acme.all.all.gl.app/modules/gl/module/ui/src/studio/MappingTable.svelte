<script lang="ts">
	// The fields one event will ask the Data Workbench for, with the mapping columns empty:
	// no mapping exists until US-015/US-016 are built, and none is invented here.
	import type { EventExplanation, ManifestEvent } from '../lib/studio';

	let { explanation, manifest }: { explanation: EventExplanation; manifest: ManifestEvent } = $props();
	let evidence = $state('');
</script>

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
			{#each explanation.fields.filter((f) => f.kind === 'SOURCE') as f (f.node)}
				{@const m = manifest.fields.find((x) => x.fieldCode === f.fieldCode && (x.listCode ?? '') === (f.listCode ?? ''))}
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
					<td><button class="btn" onclick={() => (evidence = evidence === f.node ? '' : f.node)}>Evidence</button></td>
				</tr>
				{#if evidence === f.node}
					<tr class="ev">
						<td colspan="8">
							<p class="small">
								No evidence: the Data Workbench has not been asked yet. When US-015 is built, this shows its candidate columns, sample values, join path,
								method and score exactly as received; the request will ask for <span class="mono">{f.path}</span> ({f.label}).
							</p>
						</td>
					</tr>
				{/if}
			{/each}
		</tbody>
	</table>
</div>

<style>
	.ev td {
		background: var(--gl-surface-2);
	}
	.ev p {
		margin: 0;
	}
</style>
