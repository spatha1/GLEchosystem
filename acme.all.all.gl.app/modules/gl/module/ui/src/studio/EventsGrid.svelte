<script lang="ts">
	import type { Ctx } from '../lib/ctx';
	import type { EventSummary } from '../lib/studio';

	let { ctx, events }: { ctx: Ctx; events: EventSummary[] } = $props();
	const open = (code: string) => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event: code });
</script>

<p class="muted lead">
	The business events this version configures. Select one to see what it is, where its data comes from, how its measures are calculated, which rules
	post it, which source fields it needs, and to simulate it.
</p>
{#if events.length === 0}
	<div class="card"><p class="muted">This version configures no events yet.</p></div>
{:else}
	<div class="grid3">
		{#each events as e (e.code)}
			<button class="card ev" onclick={() => open(e.code)} aria-label="Open event {e.code}">
				<div class="row top">
					<span class="code mono">{e.code}</span>
					<span class="spacer"></span>
					{#if e.errors}<span class="badge bad">{e.errors} error{e.errors === 1 ? '' : 's'}</span>{/if}
					{#if e.warnings}<span class="badge warn">{e.warnings} warning{e.warnings === 1 ? '' : 's'}</span>{/if}
				</div>
				<div class="desc">{e.description || 'No description'}</div>
				<div class="row src">
					<span class="muted small">From</span>
					{#each e.sourceSystems as s (s)}<span class="chip">{s}</span>{:else}<span class="badge warn">no source</span>{/each}
				</div>
				<div class="row src">
					<span class="muted small">Measures</span>
					{#each e.measures as m (m)}<span class="chip mono">{m}</span>{/each}
					{#each e.lists as l (l)}<span class="chip mono list">{l} [ ]</span>{/each}
				</div>
				<dl class="stats">
					<div><dt>Posting rules</dt><dd>{e.activeRules}{#if e.rules > e.activeRules}<span class="muted small"> (+{e.rules - e.activeRules} off)</span>{/if}</dd></div>
					<div><dt>Journal lines</dt><dd>{e.lines}</dd></div>
					<div><dt>Source fields</dt><dd>{e.requiredFields}</dd></div>
					<div><dt>Calculated</dt><dd>{e.derivedValues}</dd></div>
					<div><dt>Scenarios</dt><dd>{e.scenarios}</dd></div>
				</dl>
			</button>
		{/each}
	</div>
{/if}

<style>
	.lead {
		margin: 0 0 12px;
		max-width: 900px;
	}
	.ev {
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
		display: grid;
		gap: 8px;
		transition: border-color 0.12s, transform 0.12s;
	}
	.ev:hover {
		border-color: var(--gl-accent);
		transform: translateY(-1px);
	}
	.code {
		font-weight: 700;
		font-size: 15px;
		color: var(--gl-accent);
	}
	.desc {
		font-weight: 550;
	}
	.src {
		gap: 5px;
	}
	.list {
		border-style: dashed;
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(5, 1fr);
		gap: 4px;
		margin: 4px 0 0;
		padding-top: 8px;
		border-top: 1px solid var(--gl-border);
	}
	.stats div {
		display: grid;
	}
	.stats dt {
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
	}
	.stats dd {
		margin: 0;
		font-weight: 700;
		font-size: 16px;
	}
</style>
