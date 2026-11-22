// ALTB-11 — a configuration as tables, for bulk entry: an Excel workbook (one sheet per section),
// CSV files or JSON. Export writes every section; import merges the tables into a draft's content
// and says, item by item, what will change. The tables come from the same section definitions as
// the studio's forms, so a section added there is importable without new code here. Saving still
// goes through the API (one PUT, one reason), which validates, versions and audits it.
import type { Content } from './studio';
import { specs, getPath, setPath, type Field } from './sections';
import { clone } from './content';

export type ColType = 'text' | 'number' | 'bool' | 'date' | 'list' | 'numbers' | 'pairs' | 'json' | 'inactive';

export interface Column {
	key: string;
	type: ColType;
	label: string;
}

/** A table of a section: its main items, or the rows of a list inside them (e.g. a rule's lines). */
export interface TableDef {
	name: string; // postingRules, postingRules.lines, outputProfiles.sections.offsetOverrides
	section: string;
	title: string;
	path: string[]; // the row lists from the item down: [] for the items, ['lines'], ['sections', 'offsetOverrides']
	columns: Column[];
	key?: (item: any) => string; // the items' identity (main tables)
}

export interface Table {
	name: string;
	header: string[];
	rows: string[][];
}

export interface SectionChange {
	section: string;
	title: string;
	added: string[];
	changed: string[];
	removed: string[];
	unchanged: number;
}

export interface ImportResult {
	content: Content;
	sections: SectionChange[];
	problems: string[]; // nothing is saved while there is one
	ignored: string[]; // sheets that are not configuration (Contents, Columns)
}

export type ImportMode = 'merge' | 'replace';

export const PARENT = 'parent';
export const LIST_SEP = ' | ';
const JSON_SUFFIX = ':json';
const DOC_SHEETS = ['contents', 'columns', 'readme'];

// ── the tables of each section ───────────────────────────────────────────────────────────────

interface Def {
	key: string;
	type: ColType | 'rows';
	label: string;
	rows?: Def[];
}

function fromField(f: Field): Def {
	const t: Record<string, ColType | 'rows'> = { number: 'number', bool: 'bool', date: 'date', multi: 'list', strings: 'list', numbers: 'numbers', pairs: 'pairs', rows: 'rows' };
	return { key: f.key, type: t[f.type] ?? 'text', label: f.label, rows: f.fields?.filter((x) => !x.key.startsWith('_')).map(fromField) };
}

const d = (key: string, type: Def['type'] = 'text', label = key, rows?: Def[]): Def => ({ key, type, label, rows });

/** Sections with their own editors (not the generic section forms). */
const own: Record<string, { title: string; key: (x: any) => string; defs: Def[] }> = {
	postingRules: {
		title: 'Posting rules',
		key: (x) => x.code ?? '',
		defs: [
			d('code', 'text', 'Rule code'),
			d('eventType', 'text', 'Business event'),
			d('rank', 'number', 'Rank'),
			d('description', 'text', 'Description'),
			d('filter', 'text', 'Filter (formula)'),
			d('journalType', 'text', 'Journal type'),
			d('effectiveFrom', 'date', 'Effective from'),
			d('effectiveTo', 'date', 'Effective to'),
			d('active', 'inactive', 'Active (false: kept but never applied)'),
			d('lines', 'rows', 'Debit and credit lines', [
				d('code', 'text', 'Line'),
				d('side', 'text', 'Side (D or C)'),
				d('class', 'text', 'Accounting class'),
				d('amount', 'text', 'Amount (formula)'),
				d('expandBy', 'text', 'Per item of list'),
				d('entity', 'text', 'Company'),
				d('when', 'text', 'Only when (formula)'),
				d('explanation', 'text', 'Explanation')
			])
		]
	},
	scenarios: {
		title: 'Test cases (scenarios)',
		key: (x) => x.code ?? '',
		defs: [
			d('code', 'text', 'Scenario code'),
			d('description', 'text', 'Description'),
			d('eventType', 'text', 'Business event'),
			d('input.sourceSystem', 'text', 'Source system'),
			d('input.sourceEventId', 'text', 'Source record'),
			d('input.sourceEventVersion', 'number', 'Source record version'),
			d('input.transactionDate', 'date', 'Transaction date'),
			d('input.effectiveDate', 'date', 'Effective date'),
			d('input.currency', 'text', 'Currency'),
			d('input.keys', 'pairs', 'Keys'),
			d('input.dimensions', 'pairs', 'Dimensions'),
			d('input.measures', 'pairs', 'Measures'),
			d('input.lists', 'json', 'List items (JSON)'),
			d('expect', 'json', 'Expected journal (JSON)')
		]
	},
	lifecycleProfiles: {
		title: 'Lifecycle profiles',
		key: (x) => x.code ?? '',
		defs: [d('code', 'text', 'Profile code'), d('description', 'text', 'Description'), d('steps', 'list', 'Scenarios in order')]
	}
};

/** The order sections are written in: what a section refers to comes before it. */
export const SECTION_ORDER = [
	'sourceSystems',
	'keyProfiles',
	'calendars',
	'datePolicies',
	'dimensions',
	'lookups',
	'baseMeasures',
	'events',
	'sourceAuthority',
	'derivedMeasures',
	'accountingClasses',
	'accounts',
	'accountAssignmentRules',
	'journalTypes',
	'postingRules',
	'scenarios',
	'lifecycleProfiles',
	'outputLayouts',
	'outputProfiles',
	'reconciliationEquations'
];

function sectionDefs(section: string): { title: string; key: (x: any) => string; defs: Def[] } | null {
	if (own[section]) return own[section];
	const s = specs[section];
	if (s && s.section === section) return { title: s.title, key: s.key, defs: s.fields.filter((f) => !f.key.startsWith('_')).map(fromField) };
	return null;
}

const isPlain = (v: any) => v !== null && typeof v === 'object' && !Array.isArray(v);
const isEmpty = (v: any): boolean =>
	v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0) || (isPlain(v) && Object.values(v).every(isEmpty));

/** Columns for values the definitions do not know (kept on export, read back by their header). */
function extraColumns(items: any[], known: Def[]): Column[] {
	const keys = new Set(known.map((x) => x.key.split('.')[0]));
	const found = new Map<string, boolean>(); // key → every value is a string
	for (const it of items)
		for (const [k, v] of Object.entries(it ?? {}))
			if (!keys.has(k) && !isEmpty(v)) found.set(k, (found.get(k) ?? true) && typeof v === 'string');
	return [...found].map(([k, str]) => ({ key: k, type: str ? 'text' : 'json', label: k }));
}

function build(section: string, title: string, key: ((x: any) => string) | undefined, defs: Def[], path: string[], items: any[]): TableDef[] {
	const name = [section, ...path].join('.');
	const columns: Column[] = defs.filter((x) => x.type !== 'rows').map((x) => ({ key: x.key, type: x.type as ColType, label: x.label }));
	columns.push(...extraColumns(items, defs));
	const out: TableDef[] = [{ name, section, title: path.length ? `${title} › ${path.join(' › ')}` : title, path, columns, key }];
	for (const r of defs.filter((x) => x.type === 'rows')) {
		const rows = items.flatMap((it) => (getPath(it, r.key) ?? []) as any[]);
		out.push(...build(section, title, undefined, r.rows ?? [], [...path, r.key], rows));
	}
	return out;
}

/** Every table of a content: the known sections (even when empty, as a template) and any other section it holds. */
export function tableDefs(c: Content): TableDef[] {
	const sections = [...SECTION_ORDER, ...Object.keys(specs).filter((s) => specs[s].section === s && !SECTION_ORDER.includes(s))];
	for (const s of Object.keys(c)) if (!sections.includes(s)) sections.push(s);
	const out: TableDef[] = [];
	for (const s of sections) {
		const known = sectionDefs(s);
		const items = (c[s] ?? []) as any[];
		if (known) out.push(...build(s, known.title, known.key, known.defs, [], items));
		else out.push(...build(s, s, (x) => String(x?.code ?? ''), [], [], items));
	}
	return out;
}

// ── cells ────────────────────────────────────────────────────────────────────────────────────

export function formatCell(v: any, type: ColType): string {
	if (v === undefined || v === null) return '';
	switch (type) {
		case 'bool':
			return v === true ? 'true' : v === false ? 'false' : String(v);
		case 'inactive':
			return v === false ? 'false' : '';
		case 'list':
		case 'numbers':
			return Array.isArray(v) ? v.join(LIST_SEP) : String(v);
		case 'pairs':
			return isPlain(v) ? Object.entries(v).map(([k, x]) => `${k}=${x}`).join(LIST_SEP) : String(v);
		case 'json':
			return isEmpty(v) ? '' : JSON.stringify(v);
		default:
			return typeof v === 'string' ? v : String(v);
	}
}

/** An Excel date serial (days since 1899-12-30) as yyyy-mm-dd. */
function excelDate(n: number): string {
	return new Date(Math.round((n - 25569) * 86400000)).toISOString().slice(0, 10);
}

export function parseCell(raw: string, type: ColType): { value?: any; error?: string } {
	const s = (raw ?? '').trim();
	if (s === '') return {};
	switch (type) {
		case 'number': {
			const n = Number(s);
			return Number.isFinite(n) ? { value: n } : { error: `"${s}" is not a number` };
		}
		case 'bool':
			if (/^(true|yes|y|1)$/i.test(s)) return { value: true };
			if (/^(false|no|n|0)$/i.test(s)) return {};
			return { error: `"${s}" is not true or false` };
		case 'inactive':
			if (/^(false|no|n|0)$/i.test(s)) return { value: false };
			if (/^(true|yes|y|1)$/i.test(s)) return {};
			return { error: `"${s}" is not true or false` };
		case 'date':
			if (/^\d{4}-\d{2}-\d{2}$/.test(s)) return { value: s };
			if (/^\d{5}(\.\d+)?$/.test(s)) return { value: excelDate(Number(s)) };
			return { error: `"${s}" is not a date written yyyy-mm-dd` };
		case 'list':
			return { value: s.split('|').map((x) => x.trim()).filter(Boolean) };
		case 'numbers': {
			const xs = s.split('|').map((x) => x.trim()).filter(Boolean).map(Number);
			return xs.every(Number.isFinite) ? { value: xs } : { error: `"${s}" is not a list of numbers` };
		}
		case 'pairs': {
			const o: Record<string, string> = {};
			for (const part of s.split('|').map((x) => x.trim()).filter(Boolean)) {
				const i = part.indexOf('=');
				if (i <= 0) return { error: `"${part}" is not written NAME=value` };
				o[part.slice(0, i).trim()] = part.slice(i + 1).trim();
			}
			return { value: o };
		}
		case 'json':
			try {
				return { value: JSON.parse(s) };
			} catch {
				return { error: `not valid JSON: ${s.slice(0, 40)}` };
			}
		default:
			return { value: s };
	}
}

// ── export ───────────────────────────────────────────────────────────────────────────────────

const header = (c: Column) => (c.type === 'json' && !c.key.endsWith(JSON_SUFFIX) ? c.key + JSON_SUFFIX : c.key);

/** The rows of every table of a content; an empty section gives its header only (a template). */
export function toTables(c: Content): Table[] {
	return tableDefs(c).map((t) => {
		const rows: string[][] = [];
		// a row of a list names its owner: the item's key, then #n for the n-th row of each list above
		const walk = (items: any[], depth: number, owner: string) => {
			items.forEach((it, i) => {
				const id = depth === 0 ? (t.key ? t.key(it) : String(it?.code ?? '')) : `${owner}#${i + 1}`;
				if (depth === t.path.length) {
					const cells = t.columns.map((col) => formatCell(getPath(it, col.key), col.type));
					rows.push(depth === 0 ? cells : [owner, ...cells]);
				} else walk((getPath(it, t.path[depth]) ?? []) as any[], depth + 1, id);
			});
		};
		walk((c[t.section] ?? []) as any[], 0, '');
		return { name: t.name, header: t.path.length ? [PARENT, ...t.columns.map(header)] : t.columns.map(header), rows };
	});
}

/** How a column is written, for the workbook's Columns sheet. */
export function howToWrite(type: ColType): string {
	return {
		text: 'text',
		number: 'a number',
		bool: 'true or false',
		inactive: 'false to deactivate; empty when active',
		date: 'a date, yyyy-mm-dd',
		list: 'values separated by |',
		numbers: 'numbers separated by |',
		pairs: 'NAME=value pairs separated by |',
		json: 'JSON'
	}[type];
}

// ── import ───────────────────────────────────────────────────────────────────────────────────

/** Equal once empty values are ignored (an empty list or object is the same as none). */
export function same(a: any, b: any): boolean {
	if (isEmpty(a) && isEmpty(b)) return true;
	if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => same(x, b[i]));
	if (isPlain(a) && isPlain(b)) {
		const ks = new Set([...Object.keys(a), ...Object.keys(b)]);
		return [...ks].every((k) => same(a[k], b[k]));
	}
	return a === b;
}

function deletePath(o: any, key: string) {
	const ks = key.split('.');
	const parent = ks.slice(0, -1).reduce((x, k) => (x == null ? undefined : x[k]), o);
	if (parent && typeof parent === 'object') delete parent[ks[ks.length - 1]];
}

function prune(o: any): any {
	if (Array.isArray(o)) return o.map(prune);
	if (!isPlain(o)) return o;
	const out: any = {};
	for (const [k, v] of Object.entries(o)) if (!isEmpty(v) || v === false) out[k] = prune(v);
	return out;
}

interface Parsed {
	values: any; // the row's values (only non-empty ones)
	present: string[]; // the columns the table has
	rowNo: number;
}

/** Imports tables into a content: by item identity, adding new items and updating existing ones. In
 * "replace" mode a section with a main table in the file holds exactly the file's items. Columns a
 * table does not have, and sections the file does not have, are kept as they are. */
export function importTables(current: Content, tables: Table[], mode: ImportMode = 'merge'): ImportResult {
	const problems: string[] = [];
	const ignored: string[] = [];
	const sectionsInFile = new Set<string>();
	const byName = new Map<string, TableDef>();
	const content: Content = clone(current);
	for (const t of tableDefs(content)) byName.set(t.name, t);

	const parsedTables = new Map<string, Parsed[]>();
	for (const t of tables) {
		if (DOC_SHEETS.includes(t.name.toLowerCase())) {
			ignored.push(t.name);
			continue;
		}
		const def = byName.get(t.name);
		if (!def) {
			problems.push(`${t.name}: not a table of a configuration (sections are named as in the export, e.g. accounts; rows of a list as postingRules.lines)`);
			continue;
		}
		sectionsInFile.add(def.section);
		const head = t.header.map((h) => (h ?? '').trim());
		const cols = head.map((h) => {
			if (def.path.length && h === PARENT) return null;
			const key = h.endsWith(JSON_SUFFIX) ? h.slice(0, -JSON_SUFFIX.length) : h;
			return def.columns.find((c) => c.key === key) ?? (h ? { key, type: (h.endsWith(JSON_SUFFIX) ? 'json' : 'text') as ColType, label: key } : null);
		});
		if (def.path.length && !head.includes(PARENT)) {
			problems.push(`${t.name}: the first column must be "${PARENT}" (the item the row belongs to)`);
			continue;
		}
		const present = cols.filter((c): c is Column => !!c).map((c) => c.key);
		const rows: Parsed[] = [];
		t.rows.forEach((r, i) => {
			if (r.every((x) => !(x ?? '').trim())) return;
			const values: any = {};
			head.forEach((h, j) => {
				const col = cols[j];
				if (def.path.length && h === PARENT) {
					values[PARENT] = (r[j] ?? '').trim();
					return;
				}
				if (!col) return;
				const p = parseCell(r[j] ?? '', col.type);
				if (p.error) problems.push(`${t.name} row ${i + 2}, ${col.key}: ${p.error}`);
				else if (p.value !== undefined) setPath(values, col.key, p.value);
			});
			rows.push({ values, present, rowNo: i + 2 });
		});
		parsedTables.set(t.name, rows);
	}

	const sections: SectionChange[] = [];
	for (const section of [...sectionsInFile]) {
		const main = byName.get(section)!;
		const keyOf = main.key ?? ((x: any) => String(x?.code ?? ''));
		const existing = (content[section] ?? []) as any[];
		const mainRows = parsedTables.get(section);
		// the items the file speaks of, by key: from the main table, or existing ones a row table names
		const items = new Map<string, { item: any; isNew: boolean }>();
		const order: string[] = [];
		for (const r of mainRows ?? []) {
			const k = keyOf(r.values);
			if (!k || k.split('/').every((x) => !x)) {
				problems.push(`${section} row ${r.rowNo}: no ${main.columns[0]?.key ?? 'code'}`);
				continue;
			}
			if (items.has(k)) {
				problems.push(`${section} row ${r.rowNo}: ${k} is in the sheet twice`);
				continue;
			}
			const old = existing.find((x) => keyOf(x) === k);
			const merged = old ? clone(old) : {};
			for (const col of r.present) {
				const v = getPath(r.values, col);
				if (v === undefined) deletePath(merged, col);
				else if (!same(v, getPath(merged, col))) setPath(merged, col, v);
			}
			items.set(k, { item: merged, isNew: !old });
			order.push(k);
		}
		// rows of lists (e.g. a rule's lines), deepest last: each replaces the list of the item it names
		const rowTables = [...byName.values()].filter((t) => t.section === section && t.path.length && parsedTables.has(t.name)).sort((a, b) => a.path.length - b.path.length);
		for (const t of rowTables) {
			const rows = parsedTables.get(t.name)!;
			const groups = new Map<string, any[]>();
			for (const r of rows) {
				const p = r.values[PARENT] ?? '';
				delete r.values[PARENT];
				if (!p) {
					problems.push(`${t.name} row ${r.rowNo}: no ${PARENT}`);
					continue;
				}
				if (!groups.has(p)) groups.set(p, []);
				groups.get(p)!.push(r.values);
			}
			const listKey = t.path[t.path.length - 1];
			// the lists inside these rows (e.g. a file section's offset overrides): kept by position
			// unless their own sheet is in the file too
			const deeper = [...byName.values()]
				.filter((x) => x.section === section && x.path.length === t.path.length + 1 && t.path.every((p, i) => x.path[i] === p) && !parsedTables.has(x.name))
				.map((x) => x.path[x.path.length - 1]);
			// the lists this table covers: every item (or row) in the file, plus those it names
			const owners = new Map<string, any>();
			const collect = (it: any, depth: number, id: string) => {
				if (depth === t.path.length - 1) {
					owners.set(id, it);
					return;
				}
				((getPath(it, t.path[depth]) ?? []) as any[]).forEach((x, i) => collect(x, depth + 1, `${id}#${i + 1}`));
			};
			for (const [k, v] of items) collect(v.item, 0, k);
			for (const p of groups.keys()) {
				if (owners.has(p)) continue;
				const k = p.split('#')[0];
				const old = existing.find((x) => keyOf(x) === k);
				if (!old) {
					problems.push(`${t.name}: "${p}" is not an item of ${section}`);
					continue;
				}
				if (!items.has(k)) {
					items.set(k, { item: clone(old), isNew: false });
					order.push(k);
				}
				collect(items.get(k)!.item, 0, k);
				if (!owners.has(p)) problems.push(`${t.name}: "${p}" does not exist`);
			}
			for (const [id, owner] of owners) {
				const rowsFor = (groups.get(id) ?? []).map(prune);
				const oldRows = (getPath(owner, listKey) ?? []) as any[];
				// a sheet of rows replaces the rows: they have no identity of their own
				const next = rowsFor.map((r, i) => {
					const keep: any = {};
					for (const k of deeper) if (oldRows[i]?.[k] !== undefined) keep[k] = clone(oldRows[i][k]);
					return { ...r, ...keep };
				});
				if (!same(next, oldRows)) {
					if (next.length) setPath(owner, listKey, next);
					else deletePath(owner, listKey);
				}
			}
		}

		const change: SectionChange = { section, title: main.title, added: [], changed: [], removed: [], unchanged: 0 };
		const result: any[] = [];
		const seen = new Set<string>();
		for (const old of existing) {
			const k = keyOf(old);
			const got = items.get(k);
			if (got) {
				seen.add(k);
				const next = prune(got.item);
				if (same(next, old)) {
					result.push(old);
					change.unchanged++;
				} else {
					result.push(next);
					change.changed.push(k);
				}
			} else if (mode === 'replace' && mainRows) change.removed.push(k);
			else {
				result.push(old);
				change.unchanged++;
			}
		}
		for (const k of order)
			if (!seen.has(k) && items.get(k)!.isNew) {
				result.push(prune(items.get(k)!.item));
				change.added.push(k);
			}
		if (result.length) content[section] = result;
		else delete content[section];
		sections.push(change);
	}
	sections.sort((a, b) => rank(a.section) - rank(b.section));
	return { content, sections, problems, ignored };
}

const rank = (s: string) => {
	const i = SECTION_ORDER.indexOf(s);
	return i < 0 ? 999 : i;
};

/** A JSON file: a whole version (with content), a content, or some of its sections. */
export function tablesFromJson(text: string): { tables: Table[]; error?: string } {
	let data: any;
	try {
		data = JSON.parse(text);
	} catch (e) {
		return { tables: [], error: `not valid JSON: ${(e as Error).message}` };
	}
	const c = isPlain(data?.content) ? data.content : data;
	if (!isPlain(c) || !Object.values(c).every(Array.isArray)) return { tables: [], error: 'expected a configuration: { "section": [ items … ] }' };
	return { tables: toTables(c).filter((t) => t.name.split('.')[0] in c) };
}

// ── files: an Excel workbook, CSV files or JSON ──────────────────────────────────────────────

export interface SheetLike {
	name: string;
	rows: string[][];
}

/** Excel allows 31 characters and no : \ / ? * [ ] in a sheet name; the Contents sheet maps them back. */
function sheetNames(names: string[]): string[] {
	const used = new Set<string>();
	return names.map((n) => {
		const clean = n.replace(/[:\/?*[\]]/g, '_');
		let s = clean.slice(0, 31);
		for (let i = 1; used.has(s.toLowerCase()); i++) s = `${clean.slice(0, 31 - String(i).length - 1)}~${i}`;
		used.add(s.toLowerCase());
		return s;
	});
}

/** The workbook of a content: Contents (which sheet holds what), Columns (how to write each column),
 * then one sheet per table, in the order sections refer to each other. */
export function toWorkbook(c: Content): SheetLike[] {
	const tables = toTables(c);
	const defs = new Map(tableDefs(c).map((t) => [t.name, t]));
	const names = sheetNames(tables.map((t) => t.name));
	const contents: string[][] = [['sheet', 'table', 'holds', 'rows']];
	tables.forEach((t, i) => contents.push([names[i], t.name, defs.get(t.name)?.title ?? t.name, String(t.rows.length)]));
	const columns: string[][] = [['table', 'column', 'label', 'how to write it']];
	for (const t of tables) {
		const def = defs.get(t.name)!;
		if (def.path.length) columns.push([t.name, PARENT, 'The item this row belongs to (its code; #n for the n-th row of a list above)', 'text']);
		for (const col of def.columns) columns.push([t.name, header(col), col.label, howToWrite(col.type)]);
	}
	return [
		{ name: 'Contents', rows: contents },
		{ name: 'Columns', rows: columns },
		...tables.map((t, i) => ({ name: names[i], rows: [t.header, ...t.rows] }))
	];
}

/** Tables from a workbook's sheets (names mapped through its Contents sheet when it has one). */
export function tablesFromSheets(sheets: SheetLike[]): Table[] {
	const contents = sheets.find((s) => s.name.toLowerCase() === 'contents');
	const map = new Map<string, string>();
	if (contents && contents.rows[0]?.[0] === 'sheet' && contents.rows[0]?.[1] === 'table') for (const r of contents.rows.slice(1)) if (r[0] && r[1]) map.set(r[0], r[1]);
	return sheets
		.map((s) => {
			const rows = s.rows.filter((r, i) => i === 0 || r.some((x) => (x ?? '').trim()));
			return { name: map.get(s.name) ?? s.name, header: rows[0] ?? [], rows: rows.slice(1) };
		})
		.filter((t) => t.header.length || t.rows.length || DOC_SHEETS.includes(t.name.toLowerCase()));
}
