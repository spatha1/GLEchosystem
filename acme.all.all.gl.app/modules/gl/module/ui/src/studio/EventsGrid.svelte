<script lang="ts">
	import type { Ctx } from '../lib/ctx';
	import type { EventSummary } from '../lib/studio';

	let { ctx, events }: { ctx: Ctx; events: EventSummary[] } = $props();
	const open = (code: string) => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event: code });
</script>

{#if events.length === 0}
	<div class="card"><p class="muted">This version configures no events yet.</p></div>
{:else}
	<div class="grid3 evgrid">
		{#each events as e (e.code)}
			<button class="card ev" onclick={() => open(e.code)} aria-label="Open event {e.code}">
				<div class="row top">
					<span class="code mono">{e.code}</span>
					<span class="spacer"></span>
					{#if e.errors}<span class="badge bad">{e.errors} error{e.errors === 1 ? '' : 's'}</span>{/if}
					{#if e.warnings}<span class="badge warn">{e.warnings} warning{e.warnings === 1 ? '' : 's'}</span>{/if}
				</div>
				<div class="desc" title={e.description}>{e.description || 'No description'}</div>
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
	.evgrid {
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
	}
	.ev {
		font: inherit;
		color: inherit;
		text-align: left;
		cursor: pointer;
		display: grid;
		gap: 8px;
		transition:
			border-color 0.12s,
			box-shadow 0.12s;
	}
	.ev:hover {
		border-color: var(--gl-accent);
		box-shadow: var(--gl-shadow-pop);
	}
	.code {
		font-weight: 600;
		font-size: 14px;
		color: var(--gl-accent);
	}
	.desc {
		font-weight: 600;
		font-size: 15px;
		line-height: 1.4;
		display: -webkit-box;
		-webkit-line-clamp: 3;
		line-clamp: 3;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.src {
		gap: 5px;
	}
	.list {
		border-style: dashed;
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(5, minmax(0, 1fr));
		gap: 4px 10px;
		margin: 6px 0 0;
		padding-top: 12px;
		border-top: 1px solid var(--gl-border);
	}
	.stats div {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
	}
	.stats dt {
		font-size: 12px;
		line-height: 1.3;
		color: var(--gl-subtle);
	}
	.stats dd {
		margin: 2px 0 0;
		font-weight: 700;
		font-size: 18px;
	}
</style>
