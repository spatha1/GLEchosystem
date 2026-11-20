<script lang="ts">
	import { actionsFor, type ActionKind } from '../lib/permissions';
	import type { Me, Version } from '../lib/types';

	let { version, me, onaction, onpreview }: { version: Version; me: Me; onaction: (kind: ActionKind, v: Version) => void; onpreview: (v: Version) => void } =
		$props();
	const actions = $derived(actionsFor(version, me));
</script>

<div class="actions">
	{#each actions as a (a.kind)}
		<button class={a.kind} disabled={!a.enabled} title={a.why ?? ''} aria-describedby={a.why ? `why-${version.version}-${a.kind}` : undefined} onclick={() => a.enabled && onaction(a.kind, version)}>
			{a.label}
		</button>
		{#if a.why}<span class="why" id="why-{version.version}-{a.kind}">{a.why}</span>{/if}
	{/each}
	<button class="preview" onclick={() => onpreview(version)}>Preview posting</button>
</div>

<style>
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
	}
	button {
		font: inherit;
		font-size: 12px;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
	}
	.approve:not(:disabled),
	.validate:not(:disabled) {
		background: var(--gl-accent);
		border-color: var(--gl-accent);
		color: var(--gl-accent-ink);
		font-weight: 600;
	}
	.retire:not(:disabled) {
		color: var(--gl-bad);
	}
	button:disabled {
		opacity: 0.55;
		cursor: not-allowed;
	}
	button:focus-visible {
		outline: 2px solid var(--gl-accent);
		outline-offset: 1px;
	}
	.why {
		font-size: 11px;
		color: var(--gl-warn);
		max-width: 260px;
	}
</style>
