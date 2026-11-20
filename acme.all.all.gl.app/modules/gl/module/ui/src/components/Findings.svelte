<script lang="ts">
	import type { Finding } from '../lib/types';

	let { findings }: { findings: Finding[] } = $props();
	const errors = $derived(findings.filter((f) => f.severity === 'error'));
	const warnings = $derived(findings.filter((f) => f.severity !== 'error'));
</script>

{#if findings.length === 0}
	<p class="clean">No findings.</p>
{:else}
	<p class="counts">
		<span class="n bad">{errors.length} blocking</span>
		<span class="n warn">{warnings.length} warning{warnings.length === 1 ? '' : 's'}</span>
	</p>
	<ul>
		{#each [...errors, ...warnings] as f, i (i)}
			<li class={f.severity}>
				<span class="code">{f.code}</span>
				<span class="path">{f.path}</span>
				<span class="msg">{f.message}</span>
			</li>
		{/each}
	</ul>
{/if}

<style>
	.clean {
		color: var(--gl-ok);
		margin: 0;
	}
	.counts {
		display: flex;
		gap: 8px;
		margin: 0 0 8px;
		font-size: 12px;
	}
	.n {
		padding: 1px 8px;
		border-radius: 999px;
		font-weight: 600;
	}
	.n.bad {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	.n.warn {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 260px;
		overflow: auto;
	}
	li {
		display: grid;
		grid-template-columns: auto 1fr;
		column-gap: 8px;
		padding: 6px 0;
		border-top: 1px solid var(--gl-border);
		font-size: 12.5px;
	}
	.code {
		font-family: var(--gl-mono);
		font-size: 11px;
		font-weight: 600;
		padding: 1px 6px;
		border-radius: 4px;
		align-self: start;
	}
	.error .code {
		background: var(--gl-bad-soft);
		color: var(--gl-bad);
	}
	.warning .code {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
	}
	.path {
		font-family: var(--gl-mono);
		font-size: 11px;
		color: var(--gl-muted);
	}
	.msg {
		grid-column: 2;
	}
</style>
