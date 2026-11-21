// Configuring from a BRD with AI: the types the API returns, and how a step's chosen items
// become the draft's new content. The AI only proposes; the person chooses the items, enters
// what only a person may (account numbers), and the studio saves with a reason.
import type { Finding } from './types';
import type { Content } from './studio';

export type AIStepName = 'events' | 'measures' | 'formulas' | 'rules';

export const aiSteps: { key: AIStepName; n: number; label: string; what: string }[] = [
	{ key: 'events', n: 1, label: 'Business events', what: 'the financial events the BRD describes, from the insurance catalog where one fits' },
	{ key: 'measures', n: 2, label: 'Measures', what: 'the amounts the source sends for each approved event' },
	{ key: 'formulas', n: 3, label: 'Formulas', what: 'the amounts GL calculates from the measures' },
	{ key: 'rules', n: 4, label: 'Debit/credit lines (LHS = RHS)', what: 'the posting rule of each event, with synthetic test cases proving debits equal credits' }
];

export interface AIStatus {
	available: boolean;
	provider?: string;
	model?: string;
	reason?: string;
}

export interface BRDText {
	name: string;
	text: string;
}

export interface AIEvidence {
	quote: string;
	found: boolean;
}

export interface AIPart {
	code: string;
	label?: string;
	note?: string;
	evidence?: AIEvidence[];
}

export interface AIItem {
	id: string;
	section: string;
	key: string;
	group: string;
	change: 'add' | 'update';
	label: string;
	why?: string;
	confidence?: string;
	evidence: AIEvidence[];
	parts?: AIPart[];
	value: Record<string, any>;
}

export interface AIGap {
	kind: 'ACCOUNT_NUMBER' | 'SOURCE_SYSTEM' | 'DIMENSION_VALUES';
	code: string;
	name?: string;
	group?: string;
	message: string;
}

export interface AICaseLine {
	line: string;
	class: string;
	side: 'D' | 'C';
	amount: string;
	entity: string;
	account?: string;
	problem?: string;
}

export interface AICase {
	code: string;
	event: string;
	description: string;
	currency: string;
	measures: Record<string, string>;
	lines: AICaseLine[];
	skipped: string[];
	debit: string;
	credit: string;
	balanced: boolean;
	problem?: string;
}

export interface AIBalance {
	rule: string;
	event: string;
	lhs: string[];
	rhs: string[];
	conditional: string[];
}

export interface AIProposal {
	step: AIStepName;
	provider: string;
	model: string;
	brd: string;
	items: AIItem[];
	gaps: AIGap[];
	questions: string[];
	notes: string[];
	findings: Finding[];
	problems: string[];
	cases: AICase[];
	balance: AIBalance[];
	attempts: number;
	usage: { promptTokens: number; completionTokens: number };
}

/** An item's identity within its section, as the server computes it. */
export function keyOf(section: string, v: Record<string, any>): string {
	if (section === 'sourceAuthority') return `${v.eventType ?? ''}|${v.measure ?? ''}|${v.effectiveFrom ?? ''}`;
	return String(v.code ?? '');
}

/** Upserts items into content by key (a copy; content is not changed). */
export function applyItems(content: Content, items: { section: string; key: string; value: Record<string, any> }[]): Content {
	const out: Content = JSON.parse(JSON.stringify(content ?? {}));
	for (const it of items) {
		const arr = (out[it.section] ?? []) as Record<string, any>[];
		const i = arr.findIndex((x) => keyOf(it.section, x) === it.key);
		if (i >= 0) arr[i] = it.value;
		else arr.push(it.value);
		out[it.section] = arr;
	}
	return out;
}

/**
 * The items saved for a selection. The person chooses items of business events; items they
 * need that belong to no event (business keys, dimensions, new accounting classes) come with
 * them, and only when at least one event item is chosen.
 */
export function chosenItems(p: AIProposal, chosen: Set<string>): AIItem[] {
	const picked = p.items.filter((it) => it.group !== '' && chosen.has(it.id));
	if (!picked.length) return [];
	const groups = new Set(picked.map((it) => it.group));
	const shared = p.items.filter((it) => it.group === '' && needed(p, it, groups, picked));
	return [...shared, ...picked];
}

function needed(p: AIProposal, it: AIItem, groups: Set<string>, picked: AIItem[]): boolean {
	if (it.section !== 'accountingClasses') return true;
	// A new class comes only with a chosen rule that uses it.
	return picked.some((x) => x.section === 'postingRules' && (x.value.lines ?? []).some((l: any) => l.class === it.key)) || !groups.size;
}

/** The account a person entered for a gap: an account of the class and a rule assigning every line of it. */
export interface GapAccount {
	number: string;
	name: string;
}

export const accountCodeRe = /^[A-Za-z0-9._-]{1,40}$/;

export function gapItems(gaps: AIGap[], entered: Record<string, GapAccount>, activeFrom: string, content: Content): { section: string; key: string; value: Record<string, any> }[] {
	const out: { section: string; key: string; value: Record<string, any> }[] = [];
	const rules = new Set(((content.accountAssignmentRules ?? []) as any[]).map((r) => r.code));
	for (const g of gaps) {
		const a = entered[g.code];
		if (g.kind !== 'ACCOUNT_NUMBER' || !a || !accountCodeRe.test(a.number.trim())) continue;
		const number = a.number.trim();
		out.push({ section: 'accounts', key: number, value: { code: number, name: a.name.trim() || g.name || g.code, class: g.code, activeFrom } });
		let code = `AA_${g.code}`;
		for (let i = 2; rules.has(code); i++) code = `AA_${g.code}_${i}`;
		rules.add(code);
		out.push({ section: 'accountAssignmentRules', key: code, value: { code, class: g.code, account: number, description: `Every ${g.code} line posts to ${number}` } });
	}
	return out;
}

/** Base64 of a file's bytes, for the upload. */
export function toBase64(buf: ArrayBuffer): string {
	const bytes = new Uint8Array(buf);
	let s = '';
	for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
	return btoa(s);
}

/** The most text the AI steps accept: the BRD and the typed requirements together. */
export const MAX_SOURCE_CHARS = 200_000;
export const TYPED_NAME = 'Typed requirements';

/**
 * What the AI works from: the uploaded BRD, the requirements typed in the analyst's own words,
 * or both, the typed text under its own heading so the AI (and its quotes) can tell them apart.
 */
export function sourceOf(brd: BRDText | null, typed: string): BRDText | null {
	const t = typed.trim();
	if (!brd && !t) return null;
	if (!t) return { name: brd!.name, text: brd!.text };
	if (!brd) return { name: TYPED_NAME, text: t };
	return { name: `${brd.name} + typed requirements`.slice(0, 200), text: `${brd.text}\n\n## Requirements typed by the analyst\n\n${t}` };
}
