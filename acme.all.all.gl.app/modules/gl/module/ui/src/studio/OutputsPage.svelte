<script lang="ts">
	// ALTB-10 — output files: record layouts and the files made with them. Produced from a sandbox
	// book in the Ledger (fixed width, delimited or JSON lines).
	import type { Ctx } from '../lib/ctx';
	import type { Finding } from '../lib/types';
	import { specs } from '../lib/sections';
	import SectionEditor from './SectionEditor.svelte';

	let { ctx, findings }: { ctx: Ctx; findings: Finding[] } = $props();
</script>

<div class="stack">
	<SectionEditor {ctx} spec={specs.outputLayouts} {findings} />
	<SectionEditor {ctx} spec={specs.outputProfiles} {findings} />
</div>

<style>
	.stack {
		display: grid;
		gap: 14px;
	}
</style>
