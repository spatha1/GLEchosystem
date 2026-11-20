<script lang="ts">
	// The event's configuration graph as the server computed it (model.BuildEventGraph):
	// what each source field feeds, through which formulas and lookups, into which rule lines,
	// classes and accounts. Selecting an element lights up everything it reads and everything
	// that reads it.
	import { tick } from 'svelte';
	import type { Graph, GraphNode } from '../lib/studio';
	import { columns, connected, edgesFrom, edgesTo, kindLabel, nodeById, nodesIn } from '../lib/lineage';

	let { graph, known = {}, selected, onselect }: { graph: Graph; known?: Record<string, string>; selected?: string; onselect: (id: string) => void } = $props();

	let map: HTMLDivElement | undefined = $state();
	const els = new Map<string, HTMLElement>();
	let paths = $state<{ d: string; lit: boolean; mandatory: boolean; key: string }[]>([]);
	let size = $state({ w: 0, h: 0 });

	const lit = $derived(selected && nodeById(graph, selected) ? connected(graph, selected) : null);
	const sel = $derived(selected ? nodeById(graph, selected) : undefined);

	function ordered(colKey: string): GraphNode[] {
		const c = columns.find((x) => x.key === colKey)!;
		const ns = nodesIn(graph, c);
		if (colKey === 'source') {
			// Amounts first, then what classifies them, then identity and dates.
			const rank: Record<string, number> = { MEASURE: 0, DIMENSION: 2, KEY: 3, CURRENCY: 4, DATE: 5, IDENTITY: 6 };
			const r = (n: GraphNode) => (n.kind === 'LIST' ? 1 : n.list ? 1.5 : (rank[n.category ?? ''] ?? 7));
			return [...ns].sort((a, b) => r(a) - r(b));
		}
		if (colKey !== 'rules') return ns;
		const out: GraphNode[] = [];
		for (const r of ns.filter((n) => n.kind === 'RULE')) {
			out.push(r);
			out.push(...ns.filter((n) => n.kind === 'LINE' && n.rule === r.code));
		}
		out.push(...ns.filter((n) => n.kind === 'CONTROL'));
		return out;
	}

	function reg(node: HTMLElement, id: string) {
		els.set(id, node);
		return {
			destroy() {
				els.delete(id);
			}
		};
	}

	async function draw() {
		await tick();
		if (!map) return;
		const box = map.getBoundingClientRect();
		size = { w: map.scrollWidth, h: map.scrollHeight };
		const out: typeof paths = [];
		for (const e of graph.edges) {
			const a = els.get(e.from);
			const b = els.get(e.to);
			if (!a || !b) continue;
			const isLit = !!lit && lit.has(e.from) && lit.has(e.to);
			if (lit && !isLit) continue;
			const ra = a.getBoundingClientRect();
			const rb = b.getBoundingClientRect();
			const x1 = ra.right - box.left + map.scrollLeft;
			const y1 = ra.top + ra.height / 2 - box.top + map.scrollTop;
			const x2 = rb.left - box.left + map.scrollLeft;
			const y2 = rb.top + rb.height / 2 - box.top + map.scrollTop;
			let d: string;
			if (x2 > x1 + 8) {
				const dx = Math.max(30, (x2 - x1) / 2);
				d = `M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}`;
			} else {
				// same column (e.g. a list feeding a line): loop around the right edge
				const xr = Math.max(ra.right, rb.right) - box.left + map.scrollLeft + 18;
				const xb = rb.right - box.left + map.scrollLeft;
				d = `M${x1},${y1} C${xr},${y1} ${xr},${y2} ${xb},${y2}`;
			}
			out.push({ d, lit: isLit, mandatory: e.mandatory, key: `${e.from}>${e.to}>${e.role}` });
		}
		paths = out;
	}

	$effect(() => {
		graph;
		lit;
		draw();
	});

	$effect(() => {
		if (!map) return;
		const ro = new ResizeObserver(() => draw());
		ro.observe(map);
		return () => ro.disconnect();
	});

	function lineSide(n: GraphNode): string {
		return n.kind === 'LINE' ? (n.detail?.startsWith('Credit') ? 'C' : 'D') : '';
	}
	const nodeText = (id: string) => {
		const n = nodeById(graph, id);
		if (!n) return id;
		return n.kind === 'LINE' ? `${n.rule} · ${n.code}` : n.code;
	};
</script>

<p class="muted small lead">
	Every arrow is a dependency the engine uses. Source fields on the left are what the sources must supply; everything to their right is configuration. Select
	any element to trace it; solid arrows are mandatory, dashed ones optional (a filter, a condition, or a match another rule can do without).
</p>

<div class="layout">
	<div class="map card" bind:this={map}>
		<svg class="wires" width={size.w} height={size.h} aria-hidden="true">
			{#each paths as p (p.key)}
				<path d={p.d} class:lit={p.lit} class:optional={!p.mandatory} />
			{/each}
		</svg>
		{#each columns as c (c.key)}
			<div class="col">
				<div class="colhead">
					<div class="ct">{c.title}</div>
					<div class="ch">{c.hint}</div>
				</div>
				{#each ordered(c.key) as n (n.id)}
					<button
						class="node {n.kind} side{lineSide(n)}"
						class:dim={!!lit && !lit.has(n.id)}
						class:on={selected === n.id}
						class:indent={n.kind === 'LINE'}
						use:reg={n.id}
						onclick={() => onselect(n.id)}
						title={n.detail ?? n.label}
					>
						<span class="code">{n.kind === 'LINE' ? n.code : n.code}{n.list ? '' : ''}</span>
						{#if n.list}<span class="lst">in {n.list}</span>{/if}
						<span class="lab">{n.kind === 'FIELD' || n.kind === 'CLASS' || n.kind === 'ACCOUNT' || n.kind === 'CONTROL' ? n.label : (n.detail ?? n.label)}</span>
					</button>
				{:else}
					<div class="none muted small">none</div>
				{/each}
			</div>
		{/each}
	</div>

	<aside class="card panel" aria-live="polite">
		{#if sel}
			<div class="kind">{kindLabel[sel.kind]}</div>
			<h3 class="mono">{sel.kind === 'LINE' ? `${sel.rule} · ${sel.code}` : sel.code}</h3>
			<p class="lbl">{sel.label}</p>
			{#if sel.detail && sel.detail !== sel.label}<p class="mono small det">{sel.detail}</p>{/if}
			<dl class="kv small">
				{#if sel.field}<dt>Canonical field</dt><dd class="mono">{sel.field}</dd>{/if}
				{#if sel.dataType}<dt>Type</dt><dd>{sel.dataType.toLowerCase()}</dd>{/if}
				{#if sel.keyRole}<dt>Key role</dt><dd>{sel.keyRole.replace('_', ' ').toLowerCase()}</dd>{/if}
				{#if sel.path}<dt>Configured at</dt><dd class="mono">{sel.path}</dd>{/if}
			</dl>
			{#if edgesTo(graph, sel.id).length}
				<h4>Reads</h4>
				<ul class="uses">
					{#each edgesTo(graph, sel.id) as e (e.from + e.role)}
						<li>
							<button class="btn link mono" onclick={() => onselect(e.from)}>{nodeText(e.from)}</button>
							<span class="small muted">{e.label}</span>
						</li>
					{/each}
				</ul>
			{/if}
			{#if edgesFrom(graph, sel.id).length}
				<h4>Used by</h4>
				<ul class="uses">
					{#each edgesFrom(graph, sel.id) as e (e.to + e.role)}
						<li>
							<button class="btn link mono" onclick={() => onselect(e.to)}>{nodeText(e.to)}</button>
							{#if !e.mandatory}<span class="badge off">optional</span>{/if}
							<span class="small muted">{e.label}</span>
						</li>
					{/each}
				</ul>
			{:else if sel.kind === 'FIELD' || sel.kind === 'LIST'}
				<p class="notice warn small">Nothing reads this field: it is not requested from the source.</p>
			{/if}
		{:else}
			<h3>Select an element</h3>
			<p class="muted small">
				Pick a source field to see every formula, rule line, class and account it reaches — or an account to see every field that decides it.
			</p>
			<p class="muted small">{graph.nodes.length} elements · {graph.edges.length} dependencies</p>
		{/if}
	</aside>
</div>

<style>
	.lead {
		margin: 0 0 12px;
		max-width: 960px;
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
		gap: 14px;
		align-items: start;
	}
	@media (max-width: 1100px) {
		.layout {
			grid-template-columns: 1fr;
		}
	}
	.map {
		position: relative;
		display: grid;
		grid-template-columns: repeat(5, minmax(150px, 1fr));
		gap: 34px;
		overflow-x: auto;
		padding: 14px 18px;
	}
	.wires {
		position: absolute;
		left: 0;
		top: 0;
		pointer-events: none;
		overflow: visible;
	}
	.wires path {
		fill: none;
		stroke: var(--gl-border-strong);
		stroke-width: 1.2;
		opacity: 0.55;
	}
	.wires path.optional {
		stroke-dasharray: 4 3;
	}
	.wires path.lit {
		stroke: var(--gl-accent);
		stroke-width: 2;
		opacity: 0.9;
	}
	.col {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-width: 0;
		position: relative;
	}
	.colhead {
		margin-bottom: 4px;
	}
	.ct {
		font-size: 11px;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.ch {
		font-size: 11px;
		color: var(--gl-muted);
	}
	.node {
		font: inherit;
		text-align: left;
		display: grid;
		gap: 1px;
		padding: 5px 8px;
		border-radius: 6px;
		border: 1px solid var(--gl-border);
		border-left: 3px solid var(--gl-border-strong);
		background: var(--gl-surface);
		color: var(--gl-text);
		cursor: pointer;
		position: relative;
		z-index: 1;
		transition: opacity 0.12s;
	}
	.node:hover {
		border-color: var(--gl-accent);
	}
	.node.on {
		box-shadow: 0 0 0 2px var(--gl-accent);
	}
	.node.dim {
		opacity: 0.28;
	}
	.node.indent {
		margin-left: 12px;
	}
	.code {
		font-family: var(--gl-mono);
		font-weight: 700;
		font-size: 12px;
		word-break: break-all;
	}
	.lst {
		font-size: 10.5px;
		color: var(--gl-muted);
	}
	.lab {
		font-size: 11px;
		color: var(--gl-muted);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.FIELD {
		border-left-color: #2f7fb8;
	}
	.LIST {
		border-left-color: #2f7fb8;
		border-style: dashed;
		border-left-style: solid;
	}
	.DERIVED_MEASURE,
	.DERIVED_DIMENSION,
	.POLICY_VALUE {
		border-left-color: #8a5cc2;
		background: var(--gl-surface-2);
	}
	.CONTROL {
		border-left-color: var(--gl-off);
	}
	.RULE {
		border-left-color: var(--gl-text);
		background: var(--gl-surface-2);
	}
	.LINE.sideD {
		border-left-color: var(--gl-debit);
	}
	.LINE.sideC {
		border-left-color: var(--gl-credit);
	}
	.CLASS {
		border-left-color: #1f8a8a;
	}
	.ACCOUNT {
		border-left-color: var(--gl-ok);
	}
	.panel {
		position: sticky;
		top: 12px;
	}
	.kind {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--gl-muted);
		font-weight: 600;
	}
	.panel h3 {
		margin: 2px 0 4px;
		word-break: break-word;
	}
	.lbl {
		margin: 0 0 6px;
	}
	.det {
		margin: 0 0 8px;
		color: var(--gl-muted);
		word-break: break-word;
	}
	.uses {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	.uses li {
		display: grid;
		gap: 1px;
		padding-bottom: 6px;
		border-bottom: 1px solid var(--gl-border);
	}
	.uses .btn.link {
		justify-self: start;
		padding: 0;
	}
	.none {
		padding: 4px;
	}
</style>
