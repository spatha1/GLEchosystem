<script lang="ts">
	// An approved version never changes: a change is made in a new draft copied from it,
	// effective from a date, with a reason for the audit trail.
	import { nextMonth } from '../lib/content';

	let {
		from,
		busy = false,
		error = '',
		onsubmit,
		oncancel
	}: { from: number; busy?: boolean; error?: string; onsubmit: (effectiveFrom: string, reason: string) => void; oncancel: () => void } = $props();

	let effectiveFrom = $state(nextMonth());
	let reason = $state('');
	const ok = $derived(/^\d{4}-\d{2}-\d{2}$/.test(effectiveFrom) && reason.trim().length > 0);
</script>

<div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && oncancel()}>
	<div class="dialog card" role="dialog" aria-modal="true" aria-labelledby="nd-title">
		<h3 id="nd-title">New draft from version {from}</h3>
		<p class="muted small">
			Version {from} stays as it is. The draft starts as an exact copy; your changes are saved to the draft, validated, and approved by someone who did not
			edit it before they take effect.
		</p>
		<div class="fields">
			<label class="lbl">Effective from <input class="field" type="date" bind:value={effectiveFrom} /></label>
			<label class="lbl">Reason (audit trail) <textarea class="field" rows="3" bind:value={reason} placeholder="Why is this change needed?"></textarea></label>
		</div>
		{#if error}<div class="notice bad" role="alert">{error}</div>{/if}
		<div class="row actions">
			<span class="spacer"></span>
			<button class="btn" onclick={oncancel} disabled={busy}>Cancel</button>
			<button class="btn primary" disabled={!ok || busy} onclick={() => ok && onsubmit(effectiveFrom, reason.trim())}>Create draft</button>
		</div>
	</div>
</div>

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		background: rgba(10, 16, 24, 0.45);
		display: grid;
		place-items: center;
		z-index: 50;
		padding: 16px;
	}
	.dialog {
		width: min(520px, 100%);
	}
	.fields {
		display: grid;
		gap: 10px;
		margin: 12px 0;
	}
	.fields textarea {
		resize: vertical;
	}
	.actions {
		margin-top: 12px;
	}
</style>
