// Reading an event's configuration graph (model.BuildEventGraph): columns for the lineage
// map, and what a selected element is connected to.
import type { Graph, GraphEdge, GraphNode, NodeKind } from './studio';

export interface Column {
	key: string;
	title: string;
	hint: string;
	kinds: NodeKind[];
}

export const columns: Column[] = [
	{ key: 'source', title: 'Source fields', hint: 'collected from the source systems', kinds: ['FIELD', 'LIST'] },
	{ key: 'calc', title: 'Calculated by GL', hint: 'never collected', kinds: ['DERIVED_MEASURE', 'DERIVED_DIMENSION', 'POLICY_VALUE', 'LEDGER_STATE'] },
	{ key: 'rules', title: 'Rules and controls', hint: 'what reads the values', kinds: ['CONTROL', 'RULE', 'LINE'] },
	{ key: 'class', title: 'Accounting classes', hint: 'what each line posts to', kinds: ['CLASS'] },
	{ key: 'account', title: 'Accounts', hint: 'chosen by assignment rules', kinds: ['ACCOUNT'] }
];

export const kindLabel: Record<NodeKind, string> = {
	FIELD: 'Source field',
	LIST: 'List (1:N)',
	DERIVED_MEASURE: 'Formula',
	DERIVED_DIMENSION: 'Lookup',
	POLICY_VALUE: 'Date policy',
	LEDGER_STATE: 'Ledger balance',
	CONTROL: 'Control',
	RULE: 'Posting rule',
	LINE: 'Rule line',
	CLASS: 'Accounting class',
	ACCOUNT: 'Account'
};

export function nodesIn(g: Graph, c: Column): GraphNode[] {
	return g.nodes.filter((n) => c.kinds.includes(n.kind));
}

/** Everything upstream of a node (what it reads) and downstream (what reads it). */
export function connected(g: Graph, id: string): Set<string> {
	const out = new Map<string, string[]>();
	const inn = new Map<string, string[]>();
	for (const e of g.edges) {
		out.set(e.from, [...(out.get(e.from) ?? []), e.to]);
		inn.set(e.to, [...(inn.get(e.to) ?? []), e.from]);
	}
	const seen = new Set<string>([id]);
	const walk = (start: string, next: Map<string, string[]>) => {
		const stack = [start];
		while (stack.length) {
			const n = stack.pop()!;
			for (const m of next.get(n) ?? []) {
				if (!seen.has(m)) {
					seen.add(m);
					stack.push(m);
				}
			}
		}
	};
	walk(id, out);
	walk(id, inn);
	// A rule's lines belong to it.
	const node = g.nodes.find((n) => n.id === id);
	if (node?.kind === 'RULE') for (const n of g.nodes) if (n.kind === 'LINE' && n.rule === node.code) seen.add(n.id);
	if (node?.kind === 'LINE') seen.add('rule:' + node.rule);
	return seen;
}

export function edgesTo(g: Graph, id: string): GraphEdge[] {
	return g.edges.filter((e) => e.to === id);
}

export function edgesFrom(g: Graph, id: string): GraphEdge[] {
	return g.edges.filter((e) => e.from === id);
}

export function nodeById(g: Graph, id: string): GraphNode | undefined {
	return g.nodes.find((n) => n.id === id);
}
