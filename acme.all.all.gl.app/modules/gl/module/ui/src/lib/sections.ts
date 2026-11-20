// What each configuration section's items look like, for the studio's editors: their
// fields, how they are listed, and what a new one starts as. The server validates every
// item (POST …/check and validation); these specs only shape the forms.
import type { Content } from './studio';

export type FieldType = 'text' | 'code' | 'number' | 'bool' | 'date' | 'select' | 'multi' | 'strings' | 'pairs' | 'rows' | 'formula';

export interface Option {
	value: string;
	label: string;
}

export interface MeasureDef {
	code: string;
	name: string;
	category: string;
	definition: string;
	dataType: string;
	sign: string;
	grain: string;
	sourceScope: string[];
}

export interface EventTemplate {
	code: string;
	name: string;
	area: string;
	purpose: string;
	sourceKind: string;
	economicKey: string[];
	keys?: string[];
	measures: string[];
	dimensions: string[];
	lists?: { code: string; itemKey: string[]; keys?: string[]; measures?: string[] }[];
}

export interface KeyDef {
	code: string;
	description?: string;
	scope?: string[];
	links?: { to: string; cardinality: string }[];
}

export interface EventCatalog {
	catalogVersion: string;
	scope: string;
	status: string;
	areas: { code: string; name: string }[];
	events: EventTemplate[];
	keys: KeyDef[];
}

export interface Catalog {
	measures: Option[];
	dimensions: Option[];
	measureDefs?: MeasureDef[];
	events?: EventCatalog;
}

export interface Field {
	key: string; // dotted for nested objects, e.g. derivedBy.lookup
	label: string;
	type: FieldType;
	required?: boolean;
	help?: string;
	placeholder?: string;
	wide?: boolean;
	fixedOnEdit?: boolean; // the item's identity: not changed once saved
	options?: (c: Content, cat: Catalog, item: any) => Option[];
	fields?: Field[]; // for rows
	pairKeys?: (c: Content, cat: Catalog, item: any) => Option[]; // suggested keys for pairs
	newOnly?: boolean; // shown only when adding an item
	/** Runs after the user sets the field (e.g. a template filling the rest of the form). */
	onset?: (item: any, value: any, c: Content, cat: Catalog) => void;
}

export interface SectionSpec {
	section: string;
	title: string;
	noun: string; // singular, for buttons: "Add account"
	help?: string;
	key: (item: any) => string;
	fields: Field[];
	columns: { label: string; value: (item: any, c: Content) => string; mono?: boolean }[];
	blank: (c: Content) => any;
	/** Extra writes when an item is saved (e.g. an event's source authority). */
	apply?: (c: Content, item: any, original?: string, cat?: Catalog) => Content;
	/** Virtual fields read from the content when an item is opened for editing. */
	load?: (c: Content, item: any) => any;
}

const opt = (xs: string[]): Option[] => xs.map((x) => ({ value: x, label: x }));
const codes = (c: Content, section: string): string[] => ((c[section] ?? []) as any[]).map((x) => x.code);
const named = (c: Content, section: string): Option[] => ((c[section] ?? []) as any[]).map((x) => ({ value: x.code, label: x.name ? `${x.code} · ${x.name}` : x.code }));
const join = (xs?: string[]) => (xs?.length ? xs.join(', ') : '—');
const uniq = (xs: Option[]) => xs.filter((x, i) => xs.findIndex((y) => y.value === x.value) === i);

export const sourceKinds = ['POLICY', 'BILLING', 'CLAIMS', 'EARNINGS', 'SOURCE_JOURNAL', 'CUSTOM'];
export const natures = ['ASSET', 'LIABILITY', 'EQUITY', 'INCOME', 'EXPENSE'];
const weekdays = ['MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT', 'SUN'];

export function measureOptions(c: Content, cat: Catalog): Option[] {
	const custom = ((c.baseMeasures ?? []) as any[]).filter((m) => m.custom).map((m) => ({ value: m.code, label: `${m.code} · ${m.name ?? 'custom'} (custom)` }));
	return uniq([...cat.measures, ...custom]);
}
export function dimensionOptions(c: Content, cat: Catalog): Option[] {
	const configured = ((c.dimensions ?? []) as any[]).map((d) => ({ value: d.code, label: d.description ? `${d.code} · ${d.description}` : d.code }));
	return uniq([...configured, ...cat.dimensions]);
}
const keyOptions = (c: Content, cat?: Catalog) =>
	uniq([
		...opt(codes(c, 'keyProfiles')),
		...(cat?.events?.keys ?? []).map((k) => ({ value: k.code, label: `${k.code} · ${k.description ?? ''} (standard)` }))
	]);

/** The catalog keys a set of key codes needs: themselves and every key they are scoped by or linked to. */
export function keyClosure(cat: Catalog, wanted: string[]): KeyDef[] {
	const all = cat.events?.keys ?? [];
	const need = new Set<string>();
	const visit = (code: string) => {
		if (need.has(code)) return;
		need.add(code);
		const k = all.find((x) => x.code === code);
		for (const s of k?.scope ?? []) visit(s);
		for (const l of k?.links ?? []) visit(l.to);
	};
	wanted.forEach(visit);
	return all.filter((k) => need.has(k.code));
}

/** Declares the business keys and standard dimensions an event uses that the content lacks. */
export function declareUses(c: Content, ev: any, cat?: Catalog): Content {
	const out = { ...c };
	const used = [...(ev.economicKey ?? []), ...(ev.keys ?? []), ...((ev.lists ?? []) as any[]).flatMap((l) => [...(l.itemKey ?? []), ...(l.keys ?? [])])];
	const haveKeys = new Set(codes(c, 'keyProfiles'));
	const addKeys = cat ? keyClosure(cat, used).filter((k) => !haveKeys.has(k.code)) : [];
	if (addKeys.length) out.keyProfiles = [...((c.keyProfiles ?? []) as any[]), ...addKeys.map((k) => JSON.parse(JSON.stringify(k)))];
	const haveDims = new Set(codes(c, 'dimensions'));
	const standard = new Set((cat?.dimensions ?? []).map((d) => d.value));
	const addDims = ((ev.dimensions ?? []) as string[]).filter((d) => !haveDims.has(d) && standard.has(d));
	if (addDims.length) out.dimensions = [...((c.dimensions ?? []) as any[]), ...addDims.map((code) => ({ code }))];
	return out;
}

/** Fills an event form from a catalog template. */
function fromTemplate(item: any, code: string, c: Content, cat: Catalog) {
	const t = cat.events?.events.find((e) => e.code === code);
	if (!t) return;
	if (!item.code) item.code = t.code;
	item.description = t.purpose;
	item.economicKey = [...t.economicKey];
	item.keys = t.keys ? [...t.keys] : [];
	item.measures = [...t.measures];
	item.dimensions = [...t.dimensions];
	item.lists = JSON.parse(JSON.stringify(t.lists ?? []));
	const src = ((c.sourceSystems ?? []) as any[]).find((s) => s.kind === t.sourceKind);
	if (src && !item._sourceSystem) item._sourceSystem = src.code;
}
const eventOptions = (c: Content) => opt(codes(c, 'events'));

export const specs: Record<string, SectionSpec> = {
	sourceSystems: {
		section: 'sourceSystems',
		title: 'Source systems',
		noun: 'source system',
		help: 'The insurance systems GL receives data from.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. POLICY' },
			{ key: 'kind', label: 'Kind', type: 'select', required: true, options: () => opt(sourceKinds) },
			{ key: 'description', label: 'Description', type: 'text', wide: true }
		],
		columns: [
			{ label: 'Code', value: (x) => x.code, mono: true },
			{ label: 'Kind', value: (x) => x.kind },
			{ label: 'Description', value: (x) => x.description ?? '' }
		],
		blank: () => ({ code: '', kind: 'POLICY' })
	},
	sourceAuthority: {
		section: 'sourceAuthority',
		title: 'Source authority',
		noun: 'authority',
		help: 'Which source may post which measure of an event, and how. One authority per event and measure at a time.',
		key: (x) => `${x.eventType}/${x.measure}/${x.sourceSystem}/${x.effectiveFrom ?? ''}`,
		fields: [
			{ key: 'eventType', label: 'Event', type: 'select', required: true, options: eventOptions },
			{
				key: 'measure',
				label: 'Measure',
				type: 'select',
				required: true,
				options: (c, _cat, item) => [
					{ value: '*', label: '* all measures of the event' },
					...opt(((c.events ?? []) as any[]).find((e) => e.code === item.eventType)?.measures ?? [])
				]
			},
			{ key: 'sourceSystem', label: 'Source system', type: 'select', required: true, options: (c) => opt(codes(c, 'sourceSystems')) },
			{
				key: 'intakeMode',
				label: 'Intake',
				type: 'select',
				required: true,
				options: () => [
					{ value: 'RAW_EVENT', label: 'RAW_EVENT — GL builds the journal' },
					{ value: 'SOURCE_JOURNAL', label: 'SOURCE_JOURNAL — imported as balanced' }
				]
			},
			{ key: 'effectiveFrom', label: 'Effective from', type: 'date' },
			{ key: 'effectiveTo', label: 'Effective to', type: 'date' }
		],
		columns: [
			{ label: 'Event', value: (x) => x.eventType, mono: true },
			{ label: 'Measure', value: (x) => (x.measure === '*' ? 'all' : x.measure), mono: true },
			{ label: 'Source', value: (x) => x.sourceSystem },
			{ label: 'Intake', value: (x) => x.intakeMode },
			{ label: 'Effective', value: (x) => `${x.effectiveFrom ?? 'always'}${x.effectiveTo ? ` → ${x.effectiveTo}` : ''}` }
		],
		blank: () => ({ eventType: '', measure: '*', sourceSystem: '', intakeMode: 'RAW_EVENT' })
	},
	events: {
		section: 'events',
		title: 'Business events',
		noun: 'business event',
		key: (x) => x.code,
		fields: [
			{
				key: '_template',
				label: 'Start from the insurance event catalog',
				type: 'select',
				newOnly: true,
				wide: true,
				help: 'A standard general-insurance event fills the form; change anything you need, or define your own.',
				options: (_c, cat) =>
					(cat.events?.events ?? []).map((t) => ({
						value: t.code,
						label: `${cat.events?.areas.find((a) => a.code === t.area)?.name ?? t.area} · ${t.code} — ${t.name}`
					})),
				onset: (item, v, c, cat) => fromTemplate(item, v, c, cat)
			},
			{ key: 'code', label: 'Event code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. ENDORSEMENT' },
			{ key: 'description', label: 'Business purpose', type: 'text', wide: true, placeholder: 'What happened in the business' },
			{ key: '_sourceSystem', label: 'Comes from (source system)', type: 'select', options: (c) => opt(codes(c, 'sourceSystems')), help: 'Authority for all of its measures' },
			{
				key: '_intakeMode',
				label: 'Intake',
				type: 'select',
				options: () => [
					{ value: 'RAW_EVENT', label: 'RAW_EVENT — GL builds the journal' },
					{ value: 'SOURCE_JOURNAL', label: 'SOURCE_JOURNAL — imported as balanced' }
				]
			},
			{ key: 'economicKey', label: 'Economic key', type: 'multi', required: true, options: keyOptions, help: 'Identifies the economic event, whichever source sends it' },
			{ key: 'keys', label: 'Other business keys', type: 'multi', options: keyOptions, help: 'Carried for the key graph (default: the economic key)' },
			{ key: 'measures', label: 'Measures', type: 'multi', options: (c, cat) => measureOptions(c, cat) },
			{ key: 'dimensions', label: 'Dimensions', type: 'multi', options: (c, cat) => dimensionOptions(c, cat) },
			{
				key: 'lists',
				label: 'Lists (one-to-many items)',
				type: 'rows',
				wide: true,
				fields: [
					{ key: 'code', label: 'List', type: 'code', required: true, placeholder: 'ALLOCATION_LIST' },
					{ key: 'itemKey', label: 'Item key', type: 'multi', required: true, options: keyOptions },
					{ key: 'keys', label: 'Linked by', type: 'multi', options: keyOptions },
					{ key: 'measures', label: 'Item measures', type: 'multi', options: (c, cat) => measureOptions(c, cat) }
				]
			}
		],
		columns: [
			{ label: 'Event', value: (x) => x.code, mono: true },
			{ label: 'Purpose', value: (x) => x.description ?? '' },
			{ label: 'Economic key', value: (x) => join(x.economicKey), mono: true },
			{ label: 'Measures', value: (x) => join([...(x.measures ?? []), ...((x.lists ?? []) as any[]).flatMap((l) => l.measures ?? [])]), mono: true }
		],
		blank: () => ({ code: '', economicKey: [], measures: [], dimensions: [], _intakeMode: 'RAW_EVENT' }),
		load: (c, item) => {
			const a = ((c.sourceAuthority ?? []) as any[]).find((x) => x.eventType === item.code && x.measure === '*');
			return { ...item, _sourceSystem: a?.sourceSystem ?? '', _intakeMode: a?.intakeMode ?? 'RAW_EVENT' };
		},
		apply: (c, item, original, cat) => {
			const out = declareUses(c, item, cat);
			const src = item._sourceSystem;
			if (!src) return out;
			const auth = ((out.sourceAuthority ?? []) as any[]).filter((x) => !(x.eventType === (original ?? item.code) && x.measure === '*' && !x.effectiveFrom));
			auth.push({ eventType: item.code, measure: '*', sourceSystem: src, intakeMode: item._intakeMode || 'RAW_EVENT' });
			return { ...out, sourceAuthority: auth };
		}
	},
	baseMeasures: {
		section: 'baseMeasures',
		title: 'Custom measures',
		noun: 'custom measure',
		help: 'The standard catalog (PREMIUM, TAX, FEE, …) needs no entry; define a measure here only when the client has one the catalog does not.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true },
			{ key: 'custom', label: 'Custom measure (not in the catalog)', type: 'bool' },
			{ key: 'name', label: 'Name', type: 'text', required: true },
			{ key: 'definition', label: 'Definition', type: 'text', wide: true, required: true },
			{ key: 'category', label: 'Category', type: 'text', placeholder: 'e.g. PREMIUM' },
			{ key: 'dataType', label: 'Data type', type: 'select', options: () => opt(['AMOUNT', 'QUANTITY', 'RATE', 'COUNT']) },
			{ key: 'sign', label: 'Sign', type: 'select', options: () => opt(['POSITIVE', 'NEGATIVE', 'SIGNED']) },
			{ key: 'currency', label: 'Currency', type: 'select', options: () => opt(['EVENT', 'NONE']) },
			{ key: 'grain', label: 'Grain', type: 'select', options: () => opt(['EVENT', 'ITEM', 'ANY']) },
			{ key: 'sourceScope', label: 'Source kinds', type: 'multi', options: () => opt(sourceKinds) }
		],
		columns: [
			{ label: 'Measure', value: (x) => x.code, mono: true },
			{ label: 'Name', value: (x) => x.name ?? '' },
			{ label: 'Type', value: (x) => [x.dataType, x.sign, x.grain].filter(Boolean).join(' · ') }
		],
		blank: () => ({ code: '', custom: true, dataType: 'AMOUNT', sign: 'SIGNED', currency: 'EVENT', grain: 'EVENT', sourceScope: [] })
	},
	derivedMeasures: {
		section: 'derivedMeasures',
		title: 'Formulas',
		noun: 'formula',
		help: 'Calculated by GL from measures, dimensions and dates — never collected.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. GROSS' },
			{ key: 'formula', label: 'Formula', type: 'formula', required: true, wide: true, placeholder: 'e.g. PREMIUM + TAX' },
			{ key: 'resultType', label: 'Result', type: 'select', required: true, options: () => opt(['AMOUNT', 'NUMBER', 'BOOL', 'STRING', 'DATE']) },
			{ key: 'eventTypes', label: 'For events', type: 'multi', options: eventOptions, help: 'Empty: every event whose fields it can read' },
			{ key: 'description', label: 'Description', type: 'text', wide: true }
		],
		columns: [
			{ label: 'Formula', value: (x) => `${x.code} = ${x.formula}`, mono: true },
			{ label: 'Result', value: (x) => x.resultType },
			{ label: 'Events', value: (x) => join(x.eventTypes) }
		],
		blank: () => ({ code: '', formula: '', resultType: 'AMOUNT', eventTypes: [] })
	},
	dimensions: {
		section: 'dimensions',
		title: 'Dimensions',
		noun: 'dimension',
		help: 'Standard dimensions (LOB, COMPANY, PRODUCT, …) take the client’s values here; other dimensions are custom. Shared by all events.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. LOB' },
			{ key: 'custom', label: 'Custom dimension (not in the standard catalog)', type: 'bool' },
			{ key: 'description', label: 'Description', type: 'text', wide: true },
			{ key: 'values', label: 'Allowed values', type: 'strings', wide: true, placeholder: 'MOTOR, HOME — empty: any value' },
			{ key: 'derivedBy.lookup', label: 'Derived by lookup', type: 'select', options: (c) => [{ value: '', label: '— carried by the event' }, ...opt(codes(c, 'lookups'))] },
			{
				key: 'normalizations',
				label: 'Source spellings',
				type: 'rows',
				wide: true,
				fields: [
					{ key: 'sourceSystem', label: 'Source', type: 'select', required: true, options: (c) => opt(codes(c, 'sourceSystems')) },
					{ key: 'caseInsensitive', label: 'Any case', type: 'bool' },
					{ key: 'map', label: 'Source value → value', type: 'pairs' }
				]
			}
		],
		columns: [
			{ label: 'Dimension', value: (x) => x.code, mono: true },
			{ label: 'Values', value: (x) => join(x.values), mono: true },
			{ label: 'Comes from', value: (x) => (x.derivedBy?.lookup ? `lookup ${x.derivedBy.lookup}` : 'the event') }
		],
		blank: () => ({ code: '', values: [] })
	},
	lookups: {
		section: 'lookups',
		title: 'Lookups',
		noun: 'lookup',
		help: 'Derive one dimension from another, optionally by date (e.g. the banking company from the bank account).',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true },
			{ key: 'description', label: 'Description', type: 'text', wide: true },
			{ key: 'input', label: 'From dimension', type: 'select', required: true, options: (c, cat) => dimensionOptions(c, cat) },
			{ key: 'output', label: 'To dimension', type: 'select', required: true, options: (c, cat) => dimensionOptions(c, cat) },
			{
				key: 'entries',
				label: 'Entries',
				type: 'rows',
				wide: true,
				fields: [
					{ key: 'key', label: 'Input value', type: 'text', required: true },
					{ key: 'value', label: 'Output value', type: 'text', required: true },
					{ key: 'effectiveFrom', label: 'From', type: 'date' },
					{ key: 'effectiveTo', label: 'To', type: 'date' }
				]
			}
		],
		columns: [
			{ label: 'Lookup', value: (x) => x.code, mono: true },
			{ label: 'Derives', value: (x) => `${x.output} from ${x.input}`, mono: true },
			{ label: 'Entries', value: (x) => String((x.entries ?? []).length) }
		],
		blank: () => ({ code: '', input: '', output: '', entries: [] })
	},
	keyProfiles: {
		section: 'keyProfiles',
		title: 'Business keys',
		noun: 'business key',
		help: 'Policy, invoice, receipt and other identifiers: their format, scope and links.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. POLICY_NO' },
			{ key: 'description', label: 'Description', type: 'text', wide: true },
			{ key: 'pattern', label: 'Format (regular expression)', type: 'text', placeholder: 'e.g. P[0-9]+' },
			{ key: 'caseInsensitive', label: 'Any case', type: 'bool' },
			{ key: 'scope', label: 'Unique within', type: 'multi', options: keyOptions },
			{
				key: 'links',
				label: 'Links',
				type: 'rows',
				wide: true,
				fields: [
					{ key: 'to', label: 'To key', type: 'select', required: true, options: keyOptions },
					{ key: 'cardinality', label: 'Cardinality', type: 'select', required: true, options: () => opt(['MANY_TO_ONE', 'ONE_TO_ONE', 'MANY_TO_MANY']) }
				]
			}
		],
		columns: [
			{ label: 'Key', value: (x) => x.code, mono: true },
			{ label: 'Format', value: (x) => x.pattern ?? 'any', mono: true },
			{ label: 'Links to', value: (x) => join(((x.links ?? []) as any[]).map((l) => l.to)) }
		],
		blank: () => ({ code: '' })
	},
	calendars: {
		section: 'calendars',
		title: 'Calendars',
		noun: 'calendar',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true },
			{ key: 'timezone', label: 'Time zone (IANA)', type: 'text', required: true, placeholder: 'e.g. Australia/Sydney' },
			{ key: 'weekend', label: 'Weekend', type: 'multi', options: () => opt(weekdays) },
			{ key: 'holidays', label: 'Holidays', type: 'strings', wide: true, placeholder: '2026-01-01, 2026-01-26' }
		],
		columns: [
			{ label: 'Calendar', value: (x) => x.code, mono: true },
			{ label: 'Time zone', value: (x) => x.timezone },
			{ label: 'Holidays', value: (x) => String((x.holidays ?? []).length) }
		],
		blank: () => ({ code: '', timezone: '', weekend: ['SAT', 'SUN'], holidays: [] })
	},
	datePolicies: {
		section: 'datePolicies',
		title: 'Date policies',
		noun: 'date policy',
		help: 'Which date books an event, into which period, and what happens when the period is closed.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true },
			{ key: 'eventTypes', label: 'For events', type: 'multi', options: eventOptions, help: 'Empty: the default policy for every event' },
			{ key: 'calendar', label: 'Calendar', type: 'select', required: true, options: (c) => opt(codes(c, 'calendars')) },
			{ key: 'bookDate', label: 'Book on', type: 'select', required: true, options: () => opt(['TRANSACTION_DATE', 'EFFECTIVE_DATE', 'RECORDED_DATE']) },
			{ key: 'businessDayRule', label: 'Business day rule', type: 'select', options: () => opt(['NONE', 'NEXT_BUSINESS_DAY']) },
			{ key: 'fiscalYearStartMonth', label: 'Fiscal year starts (month)', type: 'number' },
			{ key: 'advance', label: 'Classify advances (effective after the book period)', type: 'bool' },
			{ key: 'lookupDate', label: 'Lookups dated by', type: 'select', options: () => opt(['EFFECTIVE_DATE', 'TRANSACTION_DATE']) },
			{ key: 'closeAfterBusinessDay', label: 'Period closes after business day', type: 'number' },
			{ key: 'latePolicy', label: 'Late events', type: 'select', options: () => opt(['NEXT_OPEN_PERIOD', 'REJECT']) }
		],
		columns: [
			{ label: 'Policy', value: (x) => x.code, mono: true },
			{ label: 'Books on', value: (x) => x.bookDate },
			{ label: 'Events', value: (x) => join(x.eventTypes) || 'all' }
		],
		blank: () => ({ code: '', calendar: '', bookDate: 'TRANSACTION_DATE' })
	},
	accountingClasses: {
		section: 'accountingClasses',
		title: 'Accounting classes',
		noun: 'accounting class',
		help: 'What a journal line is (policy receivable, premium written, tax payable, …). Posting rules name classes; accounts serve classes.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. AR0' },
			{ key: 'name', label: 'Name', type: 'text', required: true },
			{ key: 'nature', label: 'Nature', type: 'select', required: true, options: () => opt(natures) },
			{ key: 'normalSide', label: 'Normal side', type: 'select', options: () => [{ value: '', label: 'from the nature' }, { value: 'D', label: 'Debit' }, { value: 'C', label: 'Credit' }] },
			{ key: 'description', label: 'Description', type: 'text', wide: true }
		],
		columns: [
			{ label: 'Class', value: (x) => x.code, mono: true },
			{ label: 'Name', value: (x) => x.name },
			{ label: 'Nature', value: (x) => x.nature }
		],
		blank: () => ({ code: '', name: '', nature: 'ASSET' })
	},
	accounts: {
		section: 'accounts',
		title: 'Accounts (chart of accounts)',
		noun: 'account',
		help: 'The client’s own ledger account numbers. Each serves one accounting class.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Account number', type: 'text', required: true, fixedOnEdit: true, placeholder: 'e.g. 9948523' },
			{ key: 'name', label: 'Name', type: 'text', required: true },
			{ key: 'class', label: 'Accounting class', type: 'select', required: true, options: (c) => named(c, 'accountingClasses') },
			{ key: 'activeFrom', label: 'Active from', type: 'date', required: true },
			{ key: 'activeTo', label: 'Active to', type: 'date' },
			{ key: 'ledger', label: 'Ledger', type: 'text', placeholder: 'PRIMARY' },
			{ key: 'entities', label: 'Only for companies', type: 'strings', placeholder: 'C01, C09 — empty: all' },
			{ key: 'currencies', label: 'Only for currencies', type: 'strings', placeholder: 'AUD — empty: all' },
			{ key: 'subledgerKey', label: 'Subledger key on every line', type: 'select', options: (c) => [{ value: '', label: '—' }, ...keyOptions(c)] },
			{ key: 'dimensions', label: 'Dimensions on every line', type: 'multi', options: (c, cat) => dimensionOptions(c, cat) },
			{ key: 'description', label: 'Description', type: 'text', wide: true }
		],
		columns: [
			{ label: 'Account', value: (x) => x.code, mono: true },
			{ label: 'Name', value: (x) => x.name },
			{ label: 'Class', value: (x) => x.class, mono: true },
			{ label: 'Companies', value: (x) => join(x.entities) },
			{ label: 'Active', value: (x) => `${x.activeFrom ?? '…'} → ${x.activeTo ?? 'open'}` }
		],
		blank: () => ({ code: '', name: '', class: '', activeFrom: '' })
	},
	accountAssignmentRules: {
		section: 'accountAssignmentRules',
		title: 'Account assignment rules',
		noun: 'assignment rule',
		help: 'Which account a line of a class goes to, by dimension values, priority and date. A tie is refused, never guessed.',
		key: (x) => x.code,
		fields: [
			{ key: 'code', label: 'Code', type: 'code', required: true, fixedOnEdit: true, placeholder: 'e.g. AA_AR0_MOTOR' },
			{ key: 'class', label: 'Accounting class', type: 'select', required: true, options: (c) => named(c, 'accountingClasses') },
			{
				key: 'match',
				label: 'When (dimension = value; * = any value)',
				type: 'pairs',
				wide: true,
				pairKeys: (c, cat) => [...dimensionOptions(c, cat), ...opt(['ENTITY', 'CURRENCY'])]
			},
			{
				key: 'account',
				label: 'Account',
				type: 'select',
				required: true,
				options: (c, _cat, item) => ((c.accounts ?? []) as any[]).filter((a) => !item.class || a.class === item.class).map((a) => ({ value: a.code, label: `${a.code} · ${a.name}` }))
			},
			{ key: 'priority', label: 'Priority (lower first)', type: 'number' },
			{ key: 'effectiveFrom', label: 'Effective from', type: 'date' },
			{ key: 'effectiveTo', label: 'Effective to', type: 'date' },
			{ key: 'description', label: 'Description', type: 'text', wide: true }
		],
		columns: [
			{ label: 'Rule', value: (x) => x.code, mono: true },
			{ label: 'Class', value: (x) => x.class, mono: true },
			{ label: 'When', value: (x) => Object.entries(x.match ?? {}).map(([k, v]) => `${k} = ${v}`).join(' and ') || 'always (fallback)' },
			{ label: 'Account', value: (x) => x.account, mono: true },
			{ label: 'Priority', value: (x) => String(x.priority ?? 0) }
		],
		blank: () => ({ code: '', class: '', account: '', match: {} })
	}
};

// ── reading and writing nested fields ──

export function getPath(o: any, key: string): any {
	return key.split('.').reduce((x, k) => (x == null ? undefined : x[k]), o);
}

export function setPath(o: any, key: string, v: any): void {
	const ks = key.split('.');
	let x = o;
	for (const k of ks.slice(0, -1)) x = x[k] ??= {};
	x[ks[ks.length - 1]] = v;
}

function empty(v: any): boolean {
	return v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0) || (typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === 0);
}

/** An item as stored: virtual fields (starting with _) and empty values dropped, numbers as numbers. */
export function cleanItem(fields: Field[], item: any): any {
	const out: any = {};
	for (const f of fields) {
		if (f.key.startsWith('_')) continue;
		let v = getPath(item, f.key);
		if (f.type === 'number') v = v === '' || v == null ? undefined : Number(v);
		if (f.type === 'bool') v = v ? true : undefined;
		if (typeof v === 'string') v = v.trim();
		if (f.type === 'rows' && Array.isArray(v)) v = v.map((r) => cleanItem(f.fields ?? [], r)).filter((r) => !empty(r));
		if (f.type === 'pairs' && v && typeof v === 'object') v = Object.fromEntries(Object.entries(v).filter(([k]) => k.trim()));
		if (!empty(v)) setPath(out, f.key, v);
	}
	// Anything the form does not know about is kept as it was.
	for (const [k, v] of Object.entries(item)) if (!k.startsWith('_') && !fields.some((f) => f.key.split('.')[0] === k) && !empty(v)) out[k] = v;
	return out;
}

/** Content with one item of a section replaced (by its key), added, or removed (item null). */
export function withItem(c: Content, spec: SectionSpec, item: any | null, originalKey?: string, cat?: Catalog): Content {
	const items = [...((c[spec.section] ?? []) as any[])];
	const i = originalKey ? items.findIndex((x) => spec.key(x) === originalKey) : -1;
	let out: Content;
	if (item === null) {
		if (i >= 0) items.splice(i, 1);
		out = { ...c, [spec.section]: items };
	} else {
		const clean = cleanItem(spec.fields, item);
		if (i >= 0) items[i] = clean;
		else items.push(clean);
		out = { ...c, [spec.section]: items };
		if (spec.apply) out = spec.apply(out, item, originalKey, cat);
	}
	if (!(out[spec.section] as any[]).length) delete out[spec.section];
	return out;
}

/** The path of an item in a content (for its findings). */
export function itemPath(c: Content, spec: SectionSpec, key: string): string {
	const i = ((c[spec.section] ?? []) as any[]).findIndex((x) => spec.key(x) === key);
	return `${spec.section}/${i}`;
}
