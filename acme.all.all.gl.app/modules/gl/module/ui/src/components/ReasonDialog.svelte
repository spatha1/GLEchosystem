<script lang="ts">
	// Every configuration change needs a reason; it is written to the audit trail.
	let {
		title,
		verb,
		detail = '',
		busy = false,
		onsubmit,
		oncancel
	}: { title: string; verb: string; detail?: string; busy?: boolean; onsubmit: (reason: string) => void; oncancel: () => void } = $props();

	let reason = $state('');
	let box: HTMLTextAreaElement | undefined = $state();
	const ok = $derived(reason.trim().length > 0 && reason.length <= 2000);

	$effect(() => {
		box?.focus();
	});

	function key(e: KeyboardEvent) {
		if (e.key === 'Escape') oncancel();
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="scrim" onkeydown={key}>
	<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="reason-title">
		<h2 id="reason-title">{title}</h2>
		{#if detail}<p class="detail">{detail}</p>{/if}
		<label for="reason">Reason (recorded in the audit trail)</label>
		<textarea id="reason" bind:this={box} bind:value={reason} rows="3" maxlength="2000"></textarea>
		<div class="buttons">
			<button class="secondary" onclick={oncancel} disabled={busy}>Cancel</button>
			<button class="primary" onclick={() => onsubmit(reason.trim())} disabled={!ok || busy}>{busy ? 'Working…' : verb}</button>
		</div>
	</div>
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		display: grid;
		place-items: center;
		background: rgba(10, 16, 24, 0.45);
		z-index: 10;
	}
	.dialog {
		width: min(480px, calc(100vw - 32px));
		padding: 20px;
		border-radius: 12px;
		background: var(--gl-surface);
		box-shadow: 0 12px 32px rgba(0, 0, 0, 0.25);
	}
	h2 {
		margin: 0 0 6px;
		font-size: 17px;
	}
	.detail {
		margin: 0 0 12px;
		color: var(--gl-muted);
	}
	label {
		display: block;
		font-size: 12px;
		color: var(--gl-muted);
		margin-bottom: 4px;
	}
	textarea {
		width: 100%;
		box-sizing: border-box;
		font: inherit;
		padding: 8px 10px;
		border: 1px solid var(--gl-border);
		border-radius: 6px;
		background: var(--gl-surface-2);
		color: var(--gl-text);
		resize: vertical;
	}
	.buttons {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 14px;
	}
	button {
		font: inherit;
		padding: 7px 14px;
		border-radius: 6px;
		cursor: pointer;
	}
	.primary {
		border: 0;
		background: var(--gl-accent);
		color: var(--gl-accent-ink);
		font-weight: 600;
	}
	.secondary {
		border: 1px solid var(--gl-border);
		background: var(--gl-surface);
		color: var(--gl-text);
	}
	button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
</style>
