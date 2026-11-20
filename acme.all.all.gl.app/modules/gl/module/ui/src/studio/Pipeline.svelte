<script lang="ts">
	// The GL flow for one version, stage by stage, as the server reports it. Stages that are
	// not built yet say so; nothing here is inferred by the browser.
	import type { Stage } from '../lib/studio';

	let { stages, selected = '', onselect }: { stages: Stage[]; selected?: string; onselect: (s: Stage) => void } = $props();

	const icon: Record<Stage['status'], string> = { DONE: '✓', PENDING: '•', FAILED: '!', NOT_IMPLEMENTED: '–' };
	const word: Record<Stage['status'], string> = { DONE: 'Done', PENDING: 'Pending', FAILED: 'Needs attention', NOT_IMPLEMENTED: 'Not built yet' };
</script>

<ol class="flow" aria-label="GL flow for this version">
	{#each stages as s, i (s.stage)}
		<li class="stage {s.status}" class:sel={selected === s.stage}>
			<button onclick={() => onselect(s)} title={s.detail} aria-describedby="stage-{s.stage}">
				<span class="dot" aria-hidden="true">{icon[s.status]}</span>
				<span class="txt">
					<span class="lbl">{s.label}</span>
					<span class="st" id="stage-{s.stage}">{word[s.status]}</span>
				</span>
			</button>
			{#if i < stages.length - 1}<span class="arrow" aria-hidden="true"></span>{/if}
		</li>
	{/each}
</ol>

<style>
	.flow {
		list-style: none;
		margin: 0;
		padding: 2px 0 6px;
		display: flex;
		flex-wrap: wrap;
		row-gap: 8px;
	}
	.stage {
		display: flex;
		align-items: center;
		flex: 0 0 auto;
	}
	button {
		font: inherit;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 7px 10px 7px 8px;
		border-radius: 999px;
		border: 1px solid var(--gl-border);
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
		text-align: left;
	}
	button:hover {
		border-color: var(--gl-accent);
	}
	.sel button {
		box-shadow: 0 0 0 2px var(--gl-accent);
	}
	.dot {
		display: grid;
		place-items: center;
		width: 20px;
		height: 20px;
		border-radius: 50%;
		font-size: 12px;
		font-weight: 700;
		flex: none;
	}
	.txt {
		display: grid;
		line-height: 1.15;
	}
	.lbl {
		font-size: 12.5px;
		font-weight: 600;
		white-space: nowrap;
	}
	.st {
		font-size: 10.5px;
		color: var(--gl-muted);
		white-space: nowrap;
	}
	.DONE .dot {
		background: var(--gl-ok);
		color: #fff;
	}
	.PENDING .dot {
		background: var(--gl-warn-soft);
		color: var(--gl-warn);
		border: 1px solid var(--gl-warn);
	}
	.FAILED .dot {
		background: var(--gl-bad);
		color: #fff;
	}
	.FAILED .st {
		color: var(--gl-bad);
	}
	.NOT_IMPLEMENTED button {
		border-style: dashed;
		background: var(--gl-surface-2);
	}
	.NOT_IMPLEMENTED .dot {
		background: var(--gl-off-soft);
		color: var(--gl-off);
	}
	.NOT_IMPLEMENTED .lbl {
		color: var(--gl-muted);
	}
	.arrow {
		width: 14px;
		height: 2px;
		background: var(--gl-border-strong);
		margin: 0 2px;
		flex: none;
	}
</style>
