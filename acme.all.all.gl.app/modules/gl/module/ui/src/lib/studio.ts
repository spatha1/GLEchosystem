// Response shapes of the configuration studio API (service/logic/studio.go, manifest.go).
import type { AuditEntry, ConfigRef, Finding, PostingPlan, Read, Status, Version } from './types';

export interface VersionFull extends Version {
	basedOnVersion?: number;
	schemaVersion?: string;
}

export interface Derived {
	code: string;
	value: string;
	formula: string;
	formulaDigest: string;
	config: ConfigRef;
	trace: { reads: Read[] };
}

export interface ResolvedDimension {
	dimension: string;
	value: string;
	via: string;
	lookup?: string;
	key?: string;
}

export interface PeriodDecision {
	policy: string;
	bookDate: string;
	accountingPeriod: string;
	bookingClass?: string;
	reasons?: string[];
	[k: string]: unknown;
}

export interface Plan extends PostingPlan {
	derived: Derived[];
	dimensions: ResolvedDimension[];
	period?: PeriodDecision;
}

export interface EventSummary {
	code: string;
	description?: string;
	sourceSystems: string[];
	measures: string[];
	lists: string[];
	rules: number;
	activeRules: number;
	lines: number;
	requiredFields: number;
	derivedValues: number;
	scenarios: number;
	errors: number;
	warnings: number;
}

export interface EventList {
	code: string;
	itemKey: string[];
	keys?: string[];
	measures?: string[];
}

export interface EventType {
	code: string;
	description?: string;
	economicKey: string[];
	keys?: string[];
	measures?: string[];
	dimensions?: string[];
	lists?: EventList[];
}

export interface SourceAuthority {
	eventType: string;
	measure: string;
	sourceSystem: string;
	intakeMode: string;
	effectiveFrom?: string;
	effectiveTo?: string;
}

export interface MeasureUse {
	code: string;
	name: string;
	category: string;
	definition: string;
	dataType: string;
	sign: string;
	currency: string;
	grain: string;
	origin: string;
	list?: string;
	required: boolean;
}

export interface DerivedMeasure {
	code: string;
	description?: string;
	formula: string;
	resultType: string;
	eventTypes?: string[];
}

export interface DerivedUse extends DerivedMeasure {
	path: string;
	inputs: string[];
	required: boolean;
}

export interface Normalization {
	sourceSystem: string;
	caseInsensitive?: boolean;
	map: Record<string, string>;
}

export interface DimensionUse {
	code: string;
	label: string;
	carried: boolean;
	values?: string[];
	normalizations?: Normalization[];
	derivedBy?: string;
	required: boolean;
}

export interface LookupEntry {
	key: string;
	value: string;
	effectiveFrom?: string;
	effectiveTo?: string;
}

export interface Lookup {
	code: string;
	description?: string;
	input: string;
	output: string;
	entries: LookupEntry[];
}

export interface KeyUse {
	code: string;
	description?: string;
	pattern?: string;
	caseInsensitive?: boolean;
	scope?: string[];
	links?: { to: string; cardinality: string }[];
	roles: string[];
}

export interface DatePolicy {
	code: string;
	eventTypes?: string[];
	calendar: string;
	bookDate: string;
	businessDayRule?: string;
	fiscalYearStartMonth?: number;
	advance?: boolean;
	lookupDate?: string;
	closeAfterBusinessDay?: number;
	latePolicy?: string;
}

export interface Calendar {
	code: string;
	timezone: string;
	weekend?: string[];
	holidays?: string[];
}

export interface LineTemplate {
	code: string;
	class: string;
	side: 'D' | 'C';
	amount: string;
	expandBy?: string;
	entity?: string;
	when?: string;
	explanation?: string;
}

export interface PostingRule {
	code: string;
	description?: string;
	eventType: string;
	filter?: string;
	rank: number;
	lines: LineTemplate[];
	active?: boolean;
	effectiveFrom?: string;
	effectiveTo?: string;
	/** ALTB-09: the journal type of the entries it posts (see Journal types). */
	journalType?: string;
}

export interface AccountingClass {
	code: string;
	name: string;
	nature: string;
	normalSide?: string;
	description?: string;
}

export interface Account {
	code: string;
	name: string;
	class: string;
	ledger?: string;
	entities?: string[];
	currencies?: string[];
	activeFrom?: string;
	activeTo?: string;
	subledgerKey?: string;
	dimensions?: string[];
	description?: string;
}

export interface AssignmentRule {
	code: string;
	description?: string;
	class: string;
	match?: Record<string, string>;
	account: string;
	ledger?: string;
	priority?: number;
	effectiveFrom?: string;
	effectiveTo?: string;
}

export interface ClassInfo extends AccountingClass {
	normalSide: string;
	statement: string;
}

export interface Candidate {
	rule: AssignmentRule;
	path: string;
	account?: Account;
}

export interface LineView {
	line: LineTemplate;
	path: string;
	class?: ClassInfo;
	candidates: Candidate[];
}

export interface RuleView {
	rule: PostingRule;
	path: string;
	isActive: boolean;
	lines: LineView[];
}

export interface ExpectedLeg {
	class: string;
	side: 'D' | 'C';
	amount: string;
	entity?: string;
	account?: string;
}

export interface EventInput {
	sourceSystem?: string;
	sourceEventId?: string;
	sourceEventVersion?: number;
	eventType?: string;
	currency: string;
	transactionDate?: string;
	effectiveDate?: string;
	keys?: Record<string, string>;
	measures?: Record<string, string>;
	dimensions?: Record<string, string>;
	lists?: Record<string, Record<string, string>[]>;
}

export interface Scenario {
	code: string;
	description?: string;
	eventType: string;
	input: EventInput;
	expect?: { rule?: string; legs?: ExpectedLeg[]; refused?: string };
}

export type NodeKind = 'FIELD' | 'LIST' | 'DERIVED_MEASURE' | 'DERIVED_DIMENSION' | 'POLICY_VALUE' | 'LEDGER_STATE' | 'CONTROL' | 'RULE' | 'LINE' | 'CLASS' | 'ACCOUNT';

export interface GraphNode {
	id: string;
	kind: NodeKind;
	code: string;
	label: string;
	detail?: string;
	path?: string;
	field?: string;
	category?: string;
	dataType?: string;
	list?: string;
	rule?: string;
	keyRole?: string;
}

export interface GraphEdge {
	from: string;
	to: string;
	role: string;
	label: string;
	mandatory: boolean;
}

export interface Graph {
	eventType: string;
	nodes: GraphNode[];
	edges: GraphEdge[];
}

export interface Requirement {
	kind: string;
	ref: string;
}

export interface ManifestField {
	fieldCode: string;
	definition?: string;
	kind: 'SOURCE' | 'DERIVED' | 'LEDGER_STATE';
	dataType: string;
	cardinality: 'ONE' | 'LIST';
	listCode?: string;
	mandatory: boolean;
	keyRole?: string;
	derivedFrom?: string[];
	requiredBy: Requirement[];
}

export interface FieldReason {
	kind: string;
	ref: string;
	role: string;
	detail: string;
	via?: string[];
	mandatory: boolean;
	consumer: string;
}

export interface FieldExplanation {
	fieldCode: string;
	listCode?: string;
	kind: 'SOURCE' | 'DERIVED' | 'LEDGER_STATE' | 'LIST';
	category: string;
	label: string;
	path?: string;
	detail?: string;
	node: string;
	mandatory: boolean;
	sourceSystems?: string[];
	constraints?: string[];
	derivedFrom?: string[];
	reasons: FieldReason[];
}

export interface EventExplanation {
	eventType: string;
	description?: string;
	sourceSystems: string[];
	fields: FieldExplanation[];
	lists: FieldExplanation[];
	unused: FieldExplanation[];
}

export interface ManifestEvent {
	eventType: string;
	sourceSystem?: string;
	fields: ManifestField[];
}

export interface Manifest {
	schemaVersion: string;
	tenantId: string;
	configId: string;
	configVersion: number;
	configDigest: string;
	manifestDigest: string;
	events: ManifestEvent[];
}

export interface ManifestView {
	configId: string;
	version: number;
	status: Status;
	complete: boolean;
	blockingFindings: number;
	manifest: Manifest;
	explanation: { events: EventExplanation[] };
}

export interface EventView {
	event: EventType;
	path: string;
	sourceSystems: { code: string; kind: string; description?: string }[];
	authority: SourceAuthority[];
	measures: MeasureUse[];
	derived: DerivedUse[];
	dimensions: DimensionUse[];
	lookups: Lookup[];
	keys: KeyUse[];
	datePolicy?: DatePolicy;
	calendar?: Calendar;
	rules: RuleView[];
	scenarios: Scenario[];
	graph: Graph;
	manifest: ManifestEvent;
	explanation: EventExplanation;
	findings: Finding[];
}

export interface ScenarioResult {
	code: string;
	description?: string;
	eventType: string;
	/** LIFECYCLE: reads earlier postings, so it is checked as a step of a lifecycle profile (ALTB-08). */
	status: 'PASSED' | 'FAILED' | 'RAN' | 'REFUSED' | 'LIFECYCLE';
	plan?: Plan;
	error?: { code: string; message: string };
	mismatches: string[];
}

/** ALTB-07: a synthetic policy's scenarios in order, versioned with the rules. */
export interface LifecycleProfile {
	code: string;
	description?: string;
	steps: string[];
}

/** ALTB-04: one step of a lifecycle preview — a version scenario, or a synthetic event. */
export type LifecycleStepInput = { scenario: string; code?: string } | { code?: string; input: EventInput };

/** One account's balance in the temporary ledger, for one subledger key value. */
export interface LedgerBalance {
	entity: string;
	currency: string;
	account: string;
	class: string;
	key?: string;
	value?: string;
	debit: string;
	credit: string;
	/** Debit minus credit. */
	balance: string;
}

export interface LifecycleStep {
	step: number;
	code?: string;
	plan?: Plan;
	error?: { code: string; message: string };
	/** The temporary ledger after this step. */
	balances: LedgerBalance[];
	/** ALTB-08: a scenario step checked against its expectation, with the earlier steps posted. */
	check?: ScenarioResult;
}

/** A lifecycle preview: events in order against a temporary ledger. Never posted. */
export interface LifecycleResult {
	config: ConfigRef;
	steps: LifecycleStep[];
	complete: boolean;
	stopped?: string;
	lines: number;
	entries: number;
	posted: false;
}

export interface Stage {
	stage: string;
	label: string;
	status: 'DONE' | 'PENDING' | 'FAILED' | 'NOT_IMPLEMENTED';
	detail: string;
	story?: string;
}

export interface ItemChange {
	section: string;
	key: string;
	change: 'ADDED' | 'REMOVED' | 'CHANGED';
	fields: string[];
	path: string;
}

export interface FieldChange {
	fieldCode: string;
	listCode?: string;
	kind: string;
	change: 'ADDED' | 'REMOVED' | 'CHANGED';
	details: string[];
	remap: boolean;
}

export interface ScenarioImpact {
	code: string;
	eventType: string;
	before?: ScenarioResult;
	after?: ScenarioResult;
	beforeLegs: string[];
	afterLegs: string[];
	changed: boolean;
	differences: string[];
}

export interface Impact {
	from: ConfigRef;
	to: ConfigRef;
	changes: ItemChange[];
	events: { eventType: string; because: string[]; fieldsChanged: number; remap: number }[];
	manifest: { changed: boolean; fromDigest: string; toDigest: string; events: { eventType: string; change: string; fields: FieldChange[] }[] };
	scenarios: ScenarioImpact[];
	downstream: Stage[];
}

export interface AuditEntryFull extends AuditEntry {
	contentDigest?: string;
	detail?: { changes?: string[]; basedOnVersion?: string; findings?: Finding[]; [k: string]: unknown } | null;
}

/** A configuration version's content: sections of items. */
export type Content = Record<string, any[]>;

// ── ALTB-06: the Ledger, sandbox first ─────────────────────────────────────

/** A sandbox book: posts synthetic events with one approved configuration version. */
export interface SandboxBook {
	bookId: string;
	sandbox: boolean;
	configId: string;
	configVersion: number;
	contentDigest: string;
	reason: string;
	createdBy: string;
	createdAt: string;
	postings: number;
}

/** A journal entry (contract C6): one per event, legal entity and currency. */
export interface JournalEntry {
	schemaVersion: string;
	journalId: string;
	groupId: string;
	economicEventKey: string;
	eventType: string;
	legalEntity: string;
	currency: string;
	intakeMode: string;
	bookingDate: string;
	accountingPeriod: string;
	status: string;
	configId: string;
	configVersion: number;
	/** ALTB-09 */
	journalType?: string;
	/** ALTB-09: the entry this one reverses (posted on the first day of the next period). */
	reversalOf?: string;
	lines: {
		lineId: string;
		account: string;
		accountingClass: string;
		side: 'D' | 'C';
		amount: string;
		subledgerKey?: string;
		sourceRowIds: string[];
		ruleId: string;
		ruleVersion: number;
		explanation?: string;
	}[];
}

export interface SandboxPosting {
	seq: number;
	code?: string;
	sourceSystem: string;
	sourceEventId: string;
	sourceEventVersion: number;
	eventType: string;
	economicEventKey: string;
	bookDate: string;
	journals: JournalEntry[];
	postedBy: string;
	postedAt: string;
}

export interface SandboxBookView {
	book: SandboxBook;
	postings: SandboxPosting[];
	balances: LedgerBalance[];
	/** ALTB-09: the balances are as at this date (default: the last posting's book date). */
	asOf?: string;
}

export interface SandboxPostResult {
	book: SandboxBook;
	steps: { step: number; code?: string; status: 'POSTED' | 'UNCHANGED'; seq?: number; detail?: string; journals?: JournalEntry[] }[];
	posted: number;
	balances: LedgerBalance[];
}
