<script lang="ts">
	// The chart of accounts of this version: accounting classes (what a journal line is), the
	// client's account numbers serving each class, and the rules that choose an account for
	// each line. Editable in a draft.
	import type { Ctx } from '../lib/ctx';
	import type { Finding } from '../lib/types';
	import { specs } from '../lib/sections';
	import SectionEditor from './SectionEditor.svelte';

	let { ctx, findings = [] }: { ctx: Ctx; findings?: Finding[] } = $props();
</script>

<p class="muted small lead">
	Posting rules name accounting classes; each class is served by the client's accounts; assignment rules choose the account for each line by dimension,
	priority and date. A line with no matching account, or two equally good ones, is refused — never guessed.
</p>
<div class="stack">
	<SectionEditor {ctx} spec={specs.accountingClasses} {findings} />
	<SectionEditor {ctx} spec={specs.accounts} {findings} />
	<SectionEditor {ctx} spec={specs.accountAssignmentRules} {findings} />
</div>

<style>
	.lead {
		margin: 0 0 12px;
	}
</style>
