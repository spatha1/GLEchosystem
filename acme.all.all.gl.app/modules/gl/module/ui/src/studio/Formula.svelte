<script lang="ts">
	// A formula with every name it reads marked, and what that name is on hover.
	let { src, known = {} }: { src: string; known?: Record<string, string> } = $props();

	const parts = $derived.by(() => {
		const out: { t: string; id: boolean }[] = [];
		const re = /'[^']*'|[A-Z][A-Z0-9_]*(?:\.[A-Z][A-Z0-9_]*)?/g;
		let last = 0;
		for (const m of src.matchAll(re)) {
			if (m.index! > last) out.push({ t: src.slice(last, m.index), id: false });
			const isFn = /^[A-Z][A-Z0-9_]*$/.test(m[0]) && src[m.index! + m[0].length] === '(';
			out.push({ t: m[0], id: !m[0].startsWith("'") && !isFn && !['AND', 'OR', 'NOT', 'TRUE', 'FALSE', 'NULL'].includes(m[0]) });
			last = m.index! + m[0].length;
		}
		if (last < src.length) out.push({ t: src.slice(last), id: false });
		return out;
	});
</script>

<span class="f">{#each parts as p, i (i)}{#if p.id}<span class="id" class:known={!!known[p.t]} title={known[p.t] ?? p.t}>{p.t}</span>{:else}{p.t}{/if}{/each}</span>

<style>
	.f {
		font-family: var(--gl-mono);
		font-size: 12.5px;
		white-space: pre-wrap;
		word-break: break-word;
	}
	.id {
		font-weight: 650;
	}
	.id.known {
		color: var(--gl-accent);
		border-bottom: 1px dotted var(--gl-accent);
		cursor: help;
	}
</style>
