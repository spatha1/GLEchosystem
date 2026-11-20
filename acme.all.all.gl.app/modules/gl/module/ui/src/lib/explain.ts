// Plain-language explanations built from the event view the server returns. Nothing here
// knows any event, rule or account: it only words what the configuration says.
import type { EventView, LineView, RuleView } from './studio';

/** Every name a formula may read in this event, with what it is. */
export function glossary(v: EventView): Record<string, string> {
	const g: Record<string, string> = {};
	for (const m of v.measures) g[m.code] = `${m.name || m.code}: ${m.list ? `measure of each ${m.list} item` : 'measure'} (${m.dataType.toLowerCase()})`;
	for (const d of v.derived) g[d.code] = `${d.code} = ${d.formula}`;
	for (const d of v.dimensions) g[d.code] = d.carried ? `${d.label}: dimension carried by the event` : `${d.label}: derived by lookup ${d.derivedBy}`;
	for (const l of v.event.lists ?? []) {
		g[l.code] = `list of items, one per ${l.itemKey.join(' + ')}`;
		for (const m of l.measures ?? []) g[`${l.code}.${m}`] = `${m} of each ${l.code} item`;
	}
	g.TRANSACTION_DATE = 'when the transaction happened at source';
	g.EFFECTIVE_DATE = 'when the cover or change takes effect';
	if (v.datePolicy) {
		g.BOOK_DATE = `book date from date policy ${v.datePolicy.code}`;
		g.ACCOUNTING_PERIOD = `accounting period from date policy ${v.datePolicy.code}`;
		if (v.datePolicy.advance) g.BOOKING_CLASS = `A (advance) or C (current), from date policy ${v.datePolicy.code}`;
	}
	return g;
}

export function sideWord(side: string): string {
	return side === 'C' ? 'Credit' : 'Debit';
}

export function natureWord(n?: string): string {
	return (n ?? '').toLowerCase();
}

/** How a line's legal entity is decided. */
export function entityText(v: EventView, lv: LineView): string {
	const e = lv.line.entity?.trim();
	if (!e) return "the event's COMPANY";
	const d = v.dimensions.find((x) => x.code === e);
	if (d && !d.carried) {
		const lk = v.lookups.find((l) => l.code === d.derivedBy);
		return `${e}, derived by lookup ${d.derivedBy}${lk ? ` from ${lk.input}` : ''}`;
	}
	return e;
}

/** How the account of a line is chosen, in precedence order. */
export function accountText(lv: LineView): string[] {
	if (!lv.candidates.length) return ['No assignment rule chooses an account for this class: the line is refused (MISSING_ACCOUNT).'];
	return lv.candidates.map((c, i) => {
		const m = Object.entries(c.rule.match ?? {});
		const cond = m.length ? `when ${m.map(([k, x]) => `${k} = ${x === '*' ? 'any value' : x}`).join(' and ')}` : i === 0 ? 'always' : 'otherwise';
		const only = c.account?.entities?.length ? ` (open only to ${c.account.entities.join(', ')})` : '';
		const acct = c.account ? `${c.account.code} ${c.account.name}${only}` : c.rule.account;
		const prio = c.rule.priority ? ` (priority ${c.rule.priority})` : '';
		const window = c.rule.effectiveFrom || c.rule.effectiveTo ? ` [${c.rule.effectiveFrom ?? '…'} → ${c.rule.effectiveTo ?? 'open'}]` : '';
		return `${c.rule.code}${prio}: ${acct} ${cond}${window}`;
	});
}

/** One sentence per rule saying when it applies. */
export function ruleWhen(r: RuleView, event: string): string {
	const parts: string[] = [];
	parts.push(r.rule.filter?.trim() ? `applies to ${event} events where ${r.rule.filter.trim()}` : `applies to every ${event} event`);
	if (r.rule.effectiveFrom || r.rule.effectiveTo) parts.push(`booked from ${r.rule.effectiveFrom ?? 'the version start'} to ${r.rule.effectiveTo ?? 'the version end'}`);
	parts.push(`rank ${r.rule.rank} (the lowest-ranked matching rule wins; a tie is refused)`);
	return parts.join(', ');
}
