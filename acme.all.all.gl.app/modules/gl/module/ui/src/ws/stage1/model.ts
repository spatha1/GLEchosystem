// Stage 1, as pure functions over a configuration's content: reading a client's chart of
// accounts, class list and assignment matrix into configuration items (with their source row),
// mapping source classes to standard classes, and checking coverage. Deterministic: the AI may
// suggest a column mapping or a rule, but rows are read here, never invented.
import type { Content } from '../../lib/studio';
import { clone } from '../../lib/content';
import type { StandardClass } from '../api';
import { codeOf, sheetId, type LoadedSheet } from '../files';

export interface SourceRef {
	file?: string;
	sheet?: string;
	row?: number;
	values?: Record<string, string>;
	text?: string;
}
export interface Account {
	code: string;
	name: string;
	class?: string;
	classes?: string[];
	type?: string;
	normalBalance?: string;
	activeFrom: string;
	activeTo?: string;
	currencies?: string[];
	entities?: string[];
	attributes?: Record<string, string>;
	source?: SourceRef;
	[k: string]: unknown;
}
export interface ClientClass {
	code: string;
	name: string;
	nature: string;
	normalSide?: string;
	description?: string;
	standard?: string;
	custom?: boolean;
}
export interface Mapping {
	class: string;
	when?: Record<string, string>;
	note?: string;
}
export interface SourceClass {
	system: string;
	code: string;
	meaning?: string;
	role?: string;
	status?: 'UNMAPPED' | 'MAPPED' | 'NOT_USED';
	mappings?: Mapping[];
	note?: string;
	source?: SourceRef;
}
export interface Rule {
	code: string;
	description?: string;
	class: string;
	match?: Record<string, string>;
	account: string;
	ledger?: string;
	priority?: number;
	effectiveFrom?: string;
	effectiveTo?: string;
	source?: SourceRef;
}
export interface SourceFile {
	id: string;
	name: string;
	sheet?: string;
	kind: 'COA' | 'CLASSES' | 'ASSIGNMENTS' | 'OTHER';
	columns?: Record<string, string>;
	conditions?: string[];
	rows: number;
	skipped?: { row: number; reason: string }[];
	importedBy?: string;
	importedAt?: string;
}

export const accounts = (c: Content) => (c.accounts ?? []) as Account[];
export const classes = (c: Content) => (c.accountingClasses ?? []) as ClientClass[];
export const sourceClasses = (c: Content) => (c.sourceClasses ?? []) as SourceClass[];
export const rules = (c: Content) => (c.accountAssignmentRules ?? []) as Rule[];
export const sourceFiles = (c: Content) => (c.sourceFiles ?? []) as SourceFile[];

export const NATURES = ['ASSET', 'LIABILITY', 'EQUITY', 'INCOME', 'EXPENSE'];

// ── columns ──────────────────────────────────────────────────────────────────────────────────

export type Kind = SourceFile['kind'];
export const COA_ROLES = ['account', 'name', 'type', 'normalBalance', 'activeFrom', 'activeTo', 'currency', 'entity'] as const;
export const CLASS_ROLES = ['code', 'meaning', 'role', 'note'] as const;
export const roleNames: Record<string, string> = {
	account: 'Account number',
	name: 'Account name',
	type: 'Account type',
	normalBalance: 'Normal balance',
	activeFrom: 'Active from',
	activeTo: 'Active to',
	currency: 'Currency',
	entity: 'Company',
	code: 'Class code',
	meaning: 'Meaning',
	role: 'Role',
	note: 'Notes'
};

const patterns: Record<string, RegExp> = {
	account: /^(gl[ _]?)?(account|acct)([ _]?(code|no|number|id))?$/i,
	name: /^(account[ _]?)?(description|name|title)$/i,
	type: /(account[ _]?type|derived[ _]?account[ _]?type|^type$)/i,
	normalBalance: /normal[ _]?balance|^balance[ _]?side$/i,
	activeFrom: /(active|effective|valid)[ _]?from|start[ _]?date/i,
	activeTo: /(active|effective|valid)[ _]?(to|until)|end[ _]?date/i,
	currency: /^currenc(y|ies)/i,
	entity: /^(entity|company|legal[ _]?entity)$/i,
	code: /^(source[ _]?)?(accounting[ _]?)?class([ _]?code)?$/i,
	meaning: /^(meaning|class[ _]?description|description)$/i,
	role: /^role$/i,
	note: /(gl[ _]?account[ _]?use|notes?)$/i
};

/** Columns found by their headers (deterministic): role → header. */
export function detectColumns(header: string[], roles: readonly string[]): Record<string, string> {
	const out: Record<string, string> = {};
	for (const r of roles) {
		const h = header.find((x) => patterns[r]?.test(x.trim()) && !Object.values(out).includes(x));
		if (h) out[r] = h;
	}
	return out;
}

/** A column holding account numbers (not an accounting class, a use, a type or a name). */
const isAccountColumn = (h: string) => /account/i.test(h) && !/(accounting|class|type|use|name|description)/i.test(h);

/** What a sheet holds, guessed from its name and headers. */
export function detectKind(s: LoadedSheet): Kind {
	const name = `${s.file} ${s.sheet}`.toLowerCase();
	const coa = detectColumns(s.header, COA_ROLES);
	const cls = detectColumns(s.header, CLASS_ROLES);
	const accountColumns = s.header.filter(isAccountColumn);
	if (/assign/.test(name)) return 'ASSIGNMENTS';
	if (coa.account && coa.name) return 'COA';
	if (cls.code && (cls.meaning || cls.role)) return 'CLASSES';
	if (accountColumns.length >= 1 && cls.code) return 'ASSIGNMENTS';
	return 'OTHER';
}

export function normType(v: string): string | undefined {
	const s = v.trim().toLowerCase();
	if (!s) return undefined;
	if (/^asset/.test(s)) return 'ASSET';
	if (/^liab/.test(s)) return 'LIABILITY';
	if (/^(equity|capital)/.test(s)) return 'EQUITY';
	if (/^(income|revenue)/.test(s)) return 'INCOME';
	if (/^(expense|cost)/.test(s)) return 'EXPENSE';
	return undefined;
}
export function normSide(v: string): string | undefined {
	const s = v.trim().toLowerCase();
	if (/^(d|dr|debit)$/.test(s)) return 'D';
	if (/^(c|cr|credit)$/.test(s)) return 'C';
	return undefined;
}
const accountCode = /^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$/;
const isoDate = /^\d{4}-\d{2}-\d{2}$/;

const rowValues = (s: LoadedSheet, r: string[]) => Object.fromEntries(s.header.map((h, i) => [h, (r[i] ?? '').trim()]).filter(([h, v]) => h && v));

// ── the chart of accounts ────────────────────────────────────────────────────────────────────

export interface ImportPlan {
	content: Content;
	added: string[];
	changed: string[];
	unchanged: number;
	skipped: { row: number; reason: string }[];
	file: SourceFile;
}

function upsertFile(c: Content, f: SourceFile) {
	const files = sourceFiles(c).filter((x) => x.id !== f.id);
	c.sourceFiles = [...files, f];
}

const stable = (v: unknown): unknown =>
	Array.isArray(v) ? v.map(stable) : v && typeof v === 'object' ? Object.fromEntries(Object.keys(v as object).sort().map((k) => [k, stable((v as any)[k])])) : v;
const same = (a: unknown, b: unknown) => JSON.stringify(stable(a)) === JSON.stringify(stable(b));

/** Accounts from a COA sheet: each row with an account number and a name, keyed by number. */
export function importCOA(content: Content, s: LoadedSheet, cols: Record<string, string>, defaultFrom: string, actor: string): ImportPlan {
	const c = clone(content);
	const idx = (role: string) => (cols[role] ? s.header.indexOf(cols[role]) : -1);
	const used = new Set(Object.values(cols));
	const id = sheetId(s);
	const out: ImportPlan = { content: c, added: [], changed: [], unchanged: 0, skipped: [], file: undefined as unknown as SourceFile };
	const list = accounts(c);
	const seen = new Set<string>();
	s.rows.forEach((r, i) => {
		const row = i + 1;
		const get = (role: string) => (idx(role) >= 0 ? (r[idx(role)] ?? '').trim() : '');
		const code = get('account');
		const name = get('name');
		if (!code) return out.skipped.push({ row, reason: 'no account number' });
		if (!accountCode.test(code)) return out.skipped.push({ row, reason: `"${code}" is not an account number` });
		if (!name) return out.skipped.push({ row, reason: `account ${code} has no name` });
		if (seen.has(code)) return out.skipped.push({ row, reason: `account ${code} is in the sheet twice` });
		seen.add(code);
		const from = get('activeFrom');
		const to = get('activeTo');
		const attrs = Object.fromEntries(s.header.map((h, j) => [h, (r[j] ?? '').trim()]).filter(([h, v]) => h && v && !used.has(h)));
		const old = list.find((a) => a.code === code && !a.ledger);
		const next: Account = { ...(old ?? {}), code, name, activeFrom: isoDate.test(from) ? from : (old?.activeFrom ?? defaultFrom) };
		const type = normType(get('type'));
		const side = normSide(get('normalBalance'));
		if (type) next.type = type;
		if (side) next.normalBalance = side;
		if (isoDate.test(to)) next.activeTo = to;
		if (get('currency')) next.currencies = get('currency').split(/[,|; ]+/).filter(Boolean);
		if (get('entity')) next.entities = get('entity').split(/[,|; ]+/).filter(Boolean);
		if (Object.keys(attrs).length) next.attributes = attrs;
		next.source = { file: id, sheet: s.sheet || undefined, row, values: rowValues(s, r) };
		if (!old) {
			list.push(next);
			out.added.push(code);
		} else {
			const { source: _a, ...o } = old;
			const { source: _b, ...n } = next;
			if (same(o, n)) out.unchanged++;
			else out.changed.push(code);
			list[list.indexOf(old)] = next;
		}
	});
	c.accounts = list;
	out.file = { id, name: s.file, sheet: s.sheet || undefined, kind: 'COA', columns: cols, rows: s.rows.length, skipped: out.skipped.length ? out.skipped : undefined, importedBy: actor, importedAt: new Date().toISOString() };
	upsertFile(c, out.file);
	return out;
}

/** Source classes from a class list: kept exactly as received; mappings already made are kept. */
export function importClasses(content: Content, s: LoadedSheet, cols: Record<string, string>, system: string, actor: string): ImportPlan {
	const c = clone(content);
	const idx = (role: string) => (cols[role] ? s.header.indexOf(cols[role]) : -1);
	const id = sheetId(s);
	const out: ImportPlan = { content: c, added: [], changed: [], unchanged: 0, skipped: [], file: undefined as unknown as SourceFile };
	const list = sourceClasses(c);
	const seen = new Set<string>();
	s.rows.forEach((r, i) => {
		const row = i + 1;
		const get = (role: string) => (idx(role) >= 0 ? (r[idx(role)] ?? '').trim() : '');
		const code = get('code');
		if (!code) return out.skipped.push({ row, reason: 'no class code' });
		if (seen.has(code)) return out.skipped.push({ row, reason: `class ${code} is in the sheet twice` });
		seen.add(code);
		const old = list.find((x) => x.system === system && x.code === code);
		const next: SourceClass = { ...(old ?? { system, code }), system, code };
		if (get('meaning')) next.meaning = get('meaning');
		if (get('role')) next.role = get('role');
		if (get('note')) next.note = get('note');
		next.source = { file: id, sheet: s.sheet || undefined, row, values: rowValues(s, r) };
		if (!old) {
			list.push(next);
			out.added.push(code);
		} else {
			const { source: _a, ...o } = old;
			const { source: _b, ...n } = next;
			if (same(o, n)) out.unchanged++;
			else out.changed.push(code);
			list[list.indexOf(old)] = next;
		}
	});
	c.sourceClasses = list;
	out.file = { id, name: s.file, sheet: s.sheet || undefined, kind: 'CLASSES', columns: cols, rows: s.rows.length, skipped: out.skipped.length ? out.skipped : undefined, importedBy: actor, importedAt: new Date().toISOString() };
	upsertFile(c, out.file);
	return out;
}

// ── classes ──────────────────────────────────────────────────────────────────────────────────

/** Adopts a standard class into the configuration (once). */
export function adopt(c: Content, std: StandardClass): Content {
	const list = classes(c);
	if (!list.some((x) => x.code === std.code)) {
		c.accountingClasses = [...list, { code: std.code, name: std.name, nature: std.nature, normalSide: std.normalSide, description: std.definition, standard: std.code }];
	}
	return c;
}

export function setMappings(c: Content, system: string, code: string, mappings: Mapping[], status?: SourceClass['status']): Content {
	const list = sourceClasses(c);
	const i = list.findIndex((x) => x.system === system && x.code === code);
	const cur: SourceClass = i >= 0 ? { ...list[i] } : { system, code };
	cur.mappings = mappings.length ? mappings : undefined;
	cur.status = status ?? (mappings.length ? 'MAPPED' : 'UNMAPPED');
	if (i >= 0) list[i] = cur;
	else list.push(cur);
	c.sourceClasses = list;
	return c;
}

/** The client class a source class gives for a row's values (the same rule as the server's). */
export function mapSourceClass(c: Content, system: string, code: string, vals: Record<string, string>): { class?: string; why: string } {
	const s = sourceClasses(c).find((x) => x.system === system && x.code === code);
	if (!s) return { why: `${code} is not a source class of ${system}` };
	let fallback: string | undefined;
	for (const m of s.mappings ?? []) {
		if (!m.when || !Object.keys(m.when).length) {
			fallback = m.class;
			continue;
		}
		if (Object.entries(m.when).every(([k, v]) => (v === '*' ? !!vals[k]?.trim() : vals[k]?.trim() === v))) return { class: m.class, why: `mapping of ${code} when ${Object.entries(m.when).map(([k, v]) => `${k} = ${v}`).join(', ')}` };
	}
	return fallback ? { class: fallback, why: `mapping of ${code}` } : { why: `no mapping of ${code} applies` };
}

/** The classes an account serves: those it declares, else those its rules give it. */
export function servedBy(c: Content, a: Account): string[] {
	const declared = [...(a.class ? [a.class] : []), ...(a.classes ?? [])];
	if (declared.length) return declared;
	return [...new Set(rules(c).filter((r) => r.account === a.code).map((r) => r.class))];
}

// ── the assignment matrix ────────────────────────────────────────────────────────────────────

export interface MatrixColumns {
	sourceClass?: string; // header of the source class column
	system: string; // the source system its codes belong to
	clientClass?: string; // header of a column already holding client class codes
	accounts: { header: string; when: Record<string, string> }[]; // account columns, each optionally for a condition (e.g. ENTITY = X)
	conditions: Record<string, string>; // header → dimension code
	priority?: string;
	effectiveFrom?: string;
	effectiveTo?: string;
	description?: string;
}

export function detectMatrix(s: LoadedSheet, system: string): MatrixColumns {
	const h = s.header;
	const find = (re: RegExp) => h.find((x) => re.test(x));
	const acc = h.filter(isAccountColumn);
	const used = new Set<string>(acc);
	const sourceClass = find(/^(source[ _]?)?(accounting[ _]?)?class([ _]?code)?$/i);
	if (sourceClass) used.add(sourceClass);
	const cols: MatrixColumns = { system, sourceClass, accounts: acc.map((x) => ({ header: x, when: {} })), conditions: {} };
	for (const [k, re] of [
		['priority', /^(priority|precedence|rank)$/i],
		['effectiveFrom', /(effective|valid)[ _]?from/i],
		['effectiveTo', /(effective|valid)[ _]?(to|until)/i],
		['description', /(description|business[ _]?purpose|purpose)/i]
	] as const) {
		const x = find(re);
		if (x) {
			(cols as any)[k] = x;
			used.add(x);
		}
	}
	// condition columns: the source system's codes (…Code, …Type, …Culture) other than the above
	for (const x of h) if (!used.has(x) && /(code|type|culture|group|company)$/i.test(x)) cols.conditions[x] = codeOf(x);
	return cols;
}

export interface MatrixPlan {
	rules: Rule[];
	skipped: { row: number; reason: string }[];
	newDimensions: string[];
	file: SourceFile;
}

/** Assignment rules from a matrix: one per row and account column; rows that cannot be read are
 * skipped with the reason (never guessed). */
export function readMatrix(c: Content, s: LoadedSheet, m: MatrixColumns, actor: string): MatrixPlan {
	const id = sheetId(s);
	const at = (h: string | undefined, r: string[]) => (h ? (r[s.header.indexOf(h)] ?? '').trim() : '');
	const out: MatrixPlan = { rules: [], skipped: [], newDimensions: [], file: undefined as unknown as SourceFile };
	const dims = new Set(((c.dimensions ?? []) as { code: string }[]).map((d) => d.code));
	const pseudo = new Set(['ENTITY', 'CURRENCY', 'EVENT_TYPE', 'BOOKING_CLASS']);
	const known = new Set(classes(c).map((x) => x.code));
	s.rows.forEach((r, i) => {
		const row = i + 1;
		const match: Record<string, string> = {};
		for (const [h, dim] of Object.entries(m.conditions)) {
			const v = at(h, r);
			if (v) match[dim] = v === '*' || /^(all|any)$/i.test(v) ? '*' : v;
		}
		let cls = at(m.clientClass, r);
		let why = '';
		if (!cls && m.sourceClass) {
			const code = at(m.sourceClass, r);
			if (!code) return out.skipped.push({ row, reason: 'no class' });
			const got = mapSourceClass(c, m.system, code, match);
			if (!got.class) return out.skipped.push({ row, reason: `source class ${code}: ${got.why} (map it in step 2)` });
			cls = got.class;
			why = got.why;
		}
		if (!cls) return out.skipped.push({ row, reason: 'no class column' });
		if (!known.has(cls)) return out.skipped.push({ row, reason: `class ${cls} is not a class of this configuration` });
		const made: Rule[] = [];
		const problems: string[] = [];
		m.accounts.forEach((a, j) => {
			const acct = at(a.header, r);
			if (!acct) return;
			if (!accountCode.test(acct)) return problems.push(`"${acct}" in ${a.header} is not a single account number`);
			const code = `AA_${id}_${row}${m.accounts.length > 1 ? '_' + (j + 1) : ''}`;
			const rule: Rule = { code, class: cls, account: acct, match: { ...match, ...a.when } };
			if (!Object.keys(rule.match!).length) delete rule.match;
			const p = Number(at(m.priority, r));
			if (m.priority && Number.isFinite(p) && at(m.priority, r) !== '') rule.priority = p;
			if (isoDate.test(at(m.effectiveFrom, r))) rule.effectiveFrom = at(m.effectiveFrom, r);
			if (isoDate.test(at(m.effectiveTo, r))) rule.effectiveTo = at(m.effectiveTo, r);
			const d = at(m.description, r);
			rule.description = [d, why].filter(Boolean).join(' · ') || undefined;
			rule.source = { file: id, sheet: s.sheet || undefined, row, values: rowValues(s, r) };
			made.push(rule);
		});
		if (!made.length) return out.skipped.push({ row, reason: problems.join('; ') || 'no account' });
		if (problems.length) out.skipped.push({ row, reason: `partly read: ${problems.join('; ')}` });
		out.rules.push(...made);
	});
	for (const dim of new Set(Object.values(m.conditions).concat(m.accounts.flatMap((a) => Object.keys(a.when))))) if (!dims.has(dim) && !pseudo.has(dim)) out.newDimensions.push(dim);
	const fullySkipped = out.skipped.filter((x) => !x.reason.startsWith('partly read'));
	out.file = {
		id,
		name: s.file,
		sheet: s.sheet || undefined,
		kind: 'ASSIGNMENTS',
		columns: Object.fromEntries(
			[['sourceClass', m.sourceClass], ['clientClass', m.clientClass], ['priority', m.priority], ['effectiveFrom', m.effectiveFrom], ['effectiveTo', m.effectiveTo], ['description', m.description], ...m.accounts.map((a, j) => [`account${j + 1}`, a.header])].filter(
				(x): x is [string, string] => !!x[1]
			)
		),
		conditions: Object.keys(m.conditions),
		rows: s.rows.length,
		skipped: fullySkipped.length ? fullySkipped : undefined,
		importedBy: actor,
		importedAt: new Date().toISOString()
	};
	return out;
}

/** Adds the matrix's rules (replacing earlier rules from the same sheet), its condition
 * dimensions (custom, any value) and the file record. */
export function applyMatrix(content: Content, p: MatrixPlan): Content {
	const c = clone(content);
	const others = rules(c).filter((r) => r.source?.file !== p.file.id);
	c.accountAssignmentRules = [...others, ...p.rules];
	const dims = (c.dimensions ?? []) as any[];
	for (const d of p.newDimensions) dims.push({ code: d, custom: true, open: true, description: `Assignment condition from ${p.file.name}${p.file.sheet ? ' › ' + p.file.sheet : ''}` });
	if (dims.length) c.dimensions = dims;
	upsertFile(c, p.file);
	return c;
}

// ── coverage and overlaps ────────────────────────────────────────────────────────────────────

export interface Coverage {
	file: SourceFile;
	imported: number[];
	skipped: { row: number; reason: string }[];
	missing: number[];
}

/** Every row of every imported sheet: brought in (an item names it), skipped with a reason, or missing. */
export function coverage(c: Content): Coverage[] {
	const refs: SourceRef[] = [...accounts(c), ...sourceClasses(c), ...rules(c)].map((x) => x.source).filter((x): x is SourceRef => !!x?.file);
	return sourceFiles(c).map((f) => {
		const imported = [...new Set(refs.filter((r) => r.file === f.id && r.row).map((r) => r.row!))].sort((a, b) => a - b);
		const skipped = (f.skipped ?? []).filter((s) => !imported.includes(s.row));
		const missing: number[] = [];
		for (let i = 1; i <= f.rows; i++) if (!imported.includes(i) && !skipped.some((s) => s.row === i)) missing.push(i);
		return { file: f, imported, skipped, missing };
	});
}

/** Rules of the same class whose conditions are identical (they can never be told apart). */
export function overlaps(c: Content): [Rule, Rule][] {
	const out: [Rule, Rule][] = [];
	const rs = rules(c);
	const key = (r: Rule) => `${r.class}|${r.ledger ?? ''}|${r.priority ?? 0}|${r.effectiveFrom ?? ''}|${JSON.stringify(Object.entries(r.match ?? {}).sort())}`;
	const seen = new Map<string, Rule>();
	for (const r of rs) {
		const k = key(r);
		if (seen.has(k)) out.push([seen.get(k)!, r]);
		else seen.set(k, r);
	}
	return out;
}
