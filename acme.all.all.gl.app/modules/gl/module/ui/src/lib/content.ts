// Editing a version's content in the browser. Every change is applied to a copy of the
// stored content and saved as a whole through the API (PUT with the digest it started
// from), which validates, versions and audits it — the browser never decides anything.
import type { Content, DerivedMeasure, LifecycleProfile, LineTemplate, PostingRule, Scenario } from './studio';

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

/** ALTB-07: adds or replaces a lifecycle profile (a synthetic policy's scenarios in order). */
export function withProfile(content: Content, p: LifecycleProfile): Content {
	const c = clone(content);
	const list = ((c.lifecycleProfiles ?? []) as LifecycleProfile[]).filter((x) => x.code !== p.code);
	list.push(p);
	c.lifecycleProfiles = list;
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

/** Sections shared by every event: the client's reference data. */
export const referenceSections = [
	'sourceSystems',
	'baseMeasures',
	'dimensions',
	'lookups',
	'keyProfiles',
	'calendars',
	'accountingClasses',
	'accounts',
	'accountAssignmentRules',
	'reconciliationEquations',
	'outputProfiles'
];

/**
 * The part of a version to copy into a new configuration: the chosen events with what
 * belongs to them (their posting rules, source authority, scenarios, and the formulas and
 * date policies that apply to them), plus, when asked, the shared reference data.
 */
export function pickEvents(content: Content, events: string[], reference: boolean): Content {
	const keep = new Set(events);
	const out: Content = {};
	const narrow = (items: any[] | undefined) =>
		(items ?? [])
			.filter((x) => !x.eventTypes?.length || x.eventTypes.some((e: string) => keep.has(e)))
			.filter((x) => x.eventTypes?.length || keep.size > 0)
			.map((x) => (x.eventTypes?.length ? { ...clone(x), eventTypes: x.eventTypes.filter((e: string) => keep.has(e)) } : clone(x)));
	const sections: Record<string, any[]> = {
		events: (content.events ?? []).filter((e: any) => keep.has(e.code)),
		postingRules: (content.postingRules ?? []).filter((r: any) => keep.has(r.eventType)),
		sourceAuthority: (content.sourceAuthority ?? []).filter((a: any) => keep.has(a.eventType)),
		scenarios: (content.scenarios ?? []).filter((s: any) => keep.has(s.eventType)),
		// A profile is kept only when every scenario it names is kept.
		lifecycleProfiles: (content.lifecycleProfiles ?? []).filter((p: any) =>
			(p.steps ?? []).every((code: string) => (content.scenarios ?? []).some((s: any) => s.code === code && keep.has(s.eventType)))
		),
		derivedMeasures: narrow(content.derivedMeasures),
		datePolicies: narrow(content.datePolicies)
	};
	for (const [k, v] of Object.entries(sections)) if (v.length) out[k] = clone(v);
	if (reference) for (const k of referenceSections) if (content[k]?.length) out[k] = clone(content[k]);
	return out;
}
