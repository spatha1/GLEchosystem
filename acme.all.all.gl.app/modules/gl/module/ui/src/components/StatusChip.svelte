<script lang="ts">
	let { status }: { status: string } = $props();
</script>

<span class="chip {status}">{status}</span>

<style>
	.chip {
		display: inline-block;
		padding: 2px 8px;
		border-radius: 999px;
		font-size: 12px;
		font-weight: 600;
		line-height: 1.4;
		text-transform: capitalize;
		background: var(--gl-off-soft);
		color: var(--gl-off);
		border: 1px solid transparent;
	}
	.validated {
		background: var(--gl-info-soft);
		color: var(--gl-info);
	}
	.approved {
		background: var(--gl-ok-soft);
		color: var(--gl-ok);
	}
	.retired {
		text-decoration: line-through;
		opacity: 0.75;
	}
</style>
