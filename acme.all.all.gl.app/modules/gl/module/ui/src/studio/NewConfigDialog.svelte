<script lang="ts">
	// A new configuration for this client. Version 1 starts as a draft. The user chooses
	// what it starts from: nothing, or the events they tick from one of the client's
	// versions (with their rules, authority, formulas, date policies and scenarios), plus,
	// if they want it, the shared reference data. Nothing is copied without being chosen.
	import type { ConfigSummary } from '../lib/types';
	import type { Content, VersionFull } from '../lib/studio';
	import { nextMonth, pickEvents, referenceSections } from '../lib/content';

	let {
		configs,
		versions,
		load,
		busy = false,
		error = '',
		onsubmit,
		oncancel
	}: {
		configs: ConfigSummary[];
		versions: Record<string, VersionFull[]>;
		load: (configId: string, version: number) => Promise<Content>;
		busy?: boolean;
		error?: string;
		onsubmit: (r: { configId: string; title: string; effectiveFrom: string; content: Content; reason: string }) => void;
		oncancel: () => void;
	} = $props();

	const sources = $derived(configs.flatMap((c) => (versions[c.configId] ?? []).map((v) => ({ key: `${c.configId}@${v.version}`, configId: c.configId, v }))));
	let configId = $state('');
	let title = $state('');
	let effectiveFrom = $state(nextMonth());
	let start = $state(''); // "" = not chosen yet, "empty", or "<configId>@<version>"
	let source = $state<Content | null>(null);
	let picked = $state<string[]>([]);
	let reference = $state(true);
	let reason = $state('');

	$effect(() => {
		const s = sources.find((x) => x.key === start);
		source = null;
		picked = [];
		if (s) load(s.configId, s.v.version).then((c) => (source = c));
	});

	const events = $derived(((source?.events ?? []) as { code: string; description?: string }[]).map((e) => e));
	const rulesOf = (code: string) => ((source?.postingRules ?? []) as { eventType: string }[]).filter((r) => r.eventType === code).length;
	const content = $derived<Content>(start === 'empty' ? {} : source ? pickEvents(source, picked, reference) : {});
	const summary = $derived(
		Object.entries(content)
			.map(([k, v]) => `${v.length} ${k}`)
			.join(', ')
	);

	const codeOk = $derived(/^[A-Z][A-Z0-9_]{0,63}$/.test(configId));
	const taken = $derived(configs.some((c) => c.configId === configId));
	const chosen = $derived(start === 'empty' || (!!source && (picked.length > 0 || reference)));
	const ok = $derived(codeOk && !taken && chosen && /^\d{4}-\d{2}-\d{2}$/.test(effectiveFrom) && reason.trim().length > 0);

	function toggle(code: string) {
		picked = picked.includes(code) ? picked.filter((c) => c !== code) : [...picked, code];
	}
	function submit() {
		if (ok) onsubmit({ configId, title: title.trim(), effectiveFrom, content, reason: reason.trim() });
	}
</script>

<div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && oncancel()}>
	<div class="dialog card" role="dialog" aria-modal="true" aria-labelledby="nc-title">
		<h3 id="nc-title">New configuration</h3>
		<p class="muted small">Version 1 is created as a draft. It is validated, then approved by someone who did not edit it, before it applies.</p>
		<div class="fields">
			<label class="lbl"
				>Configuration code
				<input class="field mono" bind:value={configId} oninput={() => (configId = configId.toUpperCase())} placeholder="e.g. GL_MOTOR_2027" maxlength="64" />
			</label>
			{#if configId && !codeOk}<p class="hint bad">Upper-case letters, digits and _, starting with a letter.</p>{/if}
			{#if taken}<p class="hint bad">{configId} already exists for this client.</p>{/if}
			<label class="lbl">Title <input class="field" bind:value={title} placeholder="What this configuration is for" maxlength="200" /></label>
			<label class="lbl">Effective from <input class="field" type="date" bind:value={effectiveFrom} /></label>
			<label class="lbl"
				>Start from
				<select class="field" bind:value={start}>
					<option value="" disabled>Choose…</option>
					<option value="empty">Empty configuration</option>
					{#each sources as s (s.key)}<option value={s.key}>Events from {s.configId} v{s.v.version} ({s.v.status})</option>{/each}
				</select>
			</label>

			{#if start === 'empty'}
				<p class="hint">
					Nothing is copied. You then add the source systems, business events, measures and formulas, keys, dates, chart of accounts and posting
					rules in the studio.
				</p>
			{:else if start && !source}
				<p class="hint">Loading the version…</p>
			{:else if source}
				<fieldset class="pick">
					<legend>Business events to copy <span class="muted">({picked.length} of {events.length})</span></legend>
					<div class="row small">
						<button class="btn link" onclick={() => (picked = events.map((e) => e.code))}>Select all</button>
						<button class="btn link" onclick={() => (picked = [])}>Select none</button>
					</div>
					{#each events as e (e.code)}
						<label class="opt">
							<input type="checkbox" checked={picked.includes(e.code)} onchange={() => toggle(e.code)} />
							<span class="mono"><b>{e.code}</b></span>
							<span class="muted small">{e.description ?? ''} · {rulesOf(e.code)} rule{rulesOf(e.code) === 1 ? '' : 's'}</span>
						</label>
					{/each}
					<p class="hint">Each chosen event brings its posting rules, source authority, formulas, date policy and scenarios.</p>
				</fieldset>
				<label class="opt">
					<input type="checkbox" bind:checked={reference} />
					<span>Also copy the reference data</span>
					<span class="muted small">
						({referenceSections.filter((k) => source?.[k]?.length).map((k) => `${source?.[k]?.length} ${k}`).join(', ')}) — the chosen events' rules need it
					</span>
				</label>
				<p class="hint summary">{summary ? `Will copy: ${summary}.` : 'Nothing chosen yet.'}</p>
			{/if}
			<label class="lbl">Reason (audit trail) <textarea class="field" rows="2" bind:value={reason} placeholder="Why is this configuration needed?"></textarea></label>
		</div>
		{#if error}<div class="notice bad" role="alert">{error}</div>{/if}
		<div class="row actions">
			<span class="spacer"></span>
			<button class="btn" onclick={oncancel} disabled={busy}>Cancel</button>
			<button class="btn primary" disabled={!ok || busy} onclick={submit}>Create configuration</button>
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
		overflow-y: auto;
	}
	.dialog {
		width: min(620px, 100%);
	}
	.fields {
		display: grid;
		gap: 10px;
		margin: 12px 0;
	}
	.hint {
		margin: -4px 0 0;
		font-size: 12px;
		color: var(--gl-muted);
	}
	.hint.bad {
		color: var(--gl-bad);
	}
	.summary {
		color: var(--gl-text);
	}
	.pick {
		border: 1px solid var(--gl-border);
		border-radius: 8px;
		padding: 8px 12px 10px;
		display: grid;
		gap: 6px;
		margin: 0;
	}
	legend {
		font-size: 12px;
		font-weight: 650;
		padding: 0 4px;
	}
	.opt {
		display: flex;
		gap: 8px;
		align-items: baseline;
		flex-wrap: wrap;
		font-size: 13px;
	}
	textarea {
		resize: vertical;
	}
	.actions {
		margin-top: 12px;
	}
</style>
