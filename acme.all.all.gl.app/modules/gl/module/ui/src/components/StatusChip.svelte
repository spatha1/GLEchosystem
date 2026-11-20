<script lang="ts">
	let { status }: { status: string } = $props();
</script>

<span class="chip {status}">{status}</span>

<style>
	.chip {
		display: inline-block;
		padding: 1px 9px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 600;
		text-transform: capitalize;
		background: var(--gl-surface-2);
		color: var(--gl-muted);
		border: 1px solid var(--gl-border);
	}
	.validated {
		background: var(--gl-info-soft);
		color: var(--gl-info);
		border-color: transparent;
	}
	.approved {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
		border-color: transparent;
	}
	.retired {
		text-decoration: line-through;
		opacity: 0.75;
	}
</style>
