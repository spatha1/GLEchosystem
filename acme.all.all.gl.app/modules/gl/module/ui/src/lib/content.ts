// Editing a version's content in the browser. Every change is applied to a copy of the
// stored content and saved as a whole through the API (PUT with the digest it started
// from), which validates, versions and audits it — the browser never decides anything.
import type { Content, DerivedMeasure, LineTemplate, PostingRule, Scenario } from './studio';

export function clone<T>(x: T): T {
	return JSON.parse(JSON.stringify(x));
}

export function uniqueCode(base: string, taken: string[]): string {
	const b = base.toUpperCase().replace(/[^A-Z0-9_]/g, '_').replace(/^[^A-Z]+/, '') || 'NEW';
	if (!taken.includes(b)) return b;
	for (let i = 2; ; i++) if (!taken.includes(`${b}_${i}`)) return `${b}_${i}`;
}

/** A rule as stored: empty optional fields dropped, active only when false. */
export function cleanRule(r: PostingRule): PostingRule {
	const out: any = { code: r.code.trim(), eventType: r.eventType, rank: Number(r.rank) };
	if (r.description?.trim()) out.description = r.description.trim();
	if (r.filter?.trim()) out.filter = r.filter.trim();
	if (r.active === false) out.active = false;
	if (r.effectiveFrom?.trim()) out.effectiveFrom = r.effectiveFrom.trim();
	if (r.effectiveTo?.trim()) out.effectiveTo = r.effectiveTo.trim();
	out.lines = r.lines.map(cleanLine);
	return out;
}

export function cleanLine(l: LineTemplate): LineTemplate {
	const out: any = { code: l.code.trim(), class: l.class, side: l.side, amount: l.amount.trim() };
	for (const k of ['expandBy', 'entity', 'when', 'explanation'] as const) {
		const v = (l[k] ?? '').trim();
		if (v) out[k] = v;
	}
	return out;
}

/** Content with one rule replaced (by its original code), or appended when new. */
export function withRule(content: Content, rule: PostingRule, originalCode?: string): Content {
	const c = clone(content);
	const rules = (c.postingRules ?? []) as PostingRule[];
	const i = originalCode ? rules.findIndex((r) => r.code === originalCode) : -1;
	if (i >= 0) rules[i] = cleanRule(rule);
	else rules.push(cleanRule(rule));
	c.postingRules = rules;
	return c;
}

export function withoutRule(content: Content, code: string): Content {
	const c = clone(content);
	c.postingRules = ((c.postingRules ?? []) as PostingRule[]).filter((r) => r.code !== code);
	return c;
}

export function withDerived(content: Content, dm: DerivedMeasure, originalCode?: string): Content {
	const c = clone(content);
	const list = (c.derivedMeasures ?? []) as DerivedMeasure[];
	const out: any = { code: dm.code.trim(), formula: dm.formula.trim(), resultType: dm.resultType };
	if (dm.description?.trim()) out.description = dm.description.trim();
	if (dm.eventTypes?.length) out.eventTypes = dm.eventTypes;
	const i = originalCode ? list.findIndex((d) => d.code === originalCode) : -1;
	if (i >= 0) list[i] = out;
	else list.push(out);
	c.derivedMeasures = list;
	return c;
}

export function withScenario(content: Content, s: Scenario): Content {
	const c = clone(content);
	const list = ((c.scenarios ?? []) as Scenario[]).filter((x) => x.code !== s.code);
	list.push(s);
	c.scenarios = list;
	return c;
}

export function newRule(eventType: string, taken: string[], rank: number): PostingRule {
	return {
		code: uniqueCode(`PR_${eventType}_NEW`, taken),
		eventType,
		rank,
		lines: [
			{ code: 'DEBIT', class: '', side: 'D', amount: '' },
			{ code: 'CREDIT', class: '', side: 'C', amount: '' }
		]
	};
}

/** Findings that concern one configuration item (its path or anything under it). */
export function under<T extends { path: string }>(fs: T[], path: string): T[] {
	return fs.filter((f) => f.path === path || f.path.startsWith(path + '/'));
}

/** The first day of next month, the usual start of a new version. */
export function nextMonth(today = new Date()): string {
	const d = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth() + 1, 1));
	return d.toISOString().slice(0, 10);
}
