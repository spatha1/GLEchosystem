// Response shapes of the /m/gl API (see service/logic/openapi.json).

export type Role = 'GL_ADMIN' | 'CONFIG_EDITOR' | 'CONFIG_APPROVER' | 'OPERATOR' | 'VIEWER';

export interface Me {
	tenantId: string;
	actor: string;
	roles: Role[];
}

export interface ConfigSummary {
	configId: string;
	title: string;
	createdBy: string;
	createdAt: string;
	latestVersion: number;
}

export type Status = 'draft' | 'validated' | 'approved' | 'retired';

export interface Finding {
	path: string;
	code: string;
	severity: 'error' | 'warning';
	message: string;
}

export interface ValidationReport {
	ok: boolean;
	findings: Finding[];
}

export interface Version {
	tenantId: string;
	configId: string;
	version: number;
	status: Status;
	effectiveFrom: string;
	effectiveTo: string | null;
	contentDigest: string;
	createdBy: string;
	createdAt: string;
	updatedBy: string;
	updatedAt: string;
	validatedBy?: string;
	validatedAt?: string;
	approvedBy?: string;
	approvedAt?: string;
	retiredBy?: string;
	retiredAt?: string;
	editors: string[];
	content?: Record<string, unknown[]>;
	validationReport?: ValidationReport;
}

export interface AuditEntry {
	auditId: number;
	configId: string;
	version?: number;
	action: string;
	actor: string;
	reason?: string;
	fromStatus?: string;
	toStatus?: string;
	at: string;
}

export interface ConfigRef {
	configId: string;
	configVersion: number;
	contentDigest: string;
}

export interface Read {
	name: string;
	value: string;
}

export interface AssignmentMatch {
	rule: string;
	account: string;
	priority: number;
	matched: boolean;
	reason: string;
}

export interface Assignment {
	rule: string;
	account: string;
	ledger: string;
	config: ConfigRef;
	why: string[];
	considered: AssignmentMatch[];
}

export interface PlannedLine {
	lineNo: number;
	template: string;
	item?: number;
	class: string;
	side: 'D' | 'C';
	amount: string;
	currency: string;
	entity: string;
	keys: Record<string, string>;
	dimensions: Record<string, string>;
	explanation: string;
	trace: { reads: Read[] };
	account?: string;
	ledger?: string;
	assignment?: Assignment;
	problem?: string;
}

export interface RuleMatch {
	rule: string;
	rank: number;
	filter?: string;
	matched: boolean;
	reason: string;
}

export interface BalanceTotal {
	entity: string;
	currency: string;
	debit: string;
	credit: string;
	balanced: boolean;
}

export interface PostingPlan {
	eventType: string;
	rule: string;
	rank: number;
	config: ConfigRef;
	considered: RuleMatch[];
	lines: PlannedLine[];
	skipped: string[];
	totals: BalanceTotal[];
	balanced: boolean;
	bookDate?: string;
}
