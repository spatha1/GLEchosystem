// The new workspace's API client: everything the studio's client has, plus onboarding, the
// standard class library, rule testing, return to draft and the AI assistant. One client per
// tenant: the server checks every call against the caller's roles in that tenant.
import { BASE, client, toError, type Fetcher } from '../lib/api';
import type { Finding } from '../lib/types';
import type { VersionFull } from '../lib/studio';

export interface StandardClass {
	code: string;
	group: string;
	name: string;
	definition: string;
	nature: string;
	normalSide: string;
	status: 'PROPOSED' | 'APPROVED' | 'RETIRED';
}
export interface ClassLibrary {
	catalog: string;
	version: string;
	status: string;
	note: string;
	classes: StandardClass[];
}
export interface ClientProfile {
	tenantId: string;
	name: string;
	status: 'ONBOARDING' | 'ACTIVE' | 'SUSPENDED';
	configId?: string;
	profile: OnboardingProfile;
	createdBy: string;
	createdAt: string;
	updatedBy: string;
	updatedAt: string;
}
export interface OnboardingProfile {
	description?: string;
	sourceSystems?: string[];
	parameters?: Record<string, string>;
	team?: { user: string; roles: string[] }[];
	reference?: string;
	documents?: string[];
	effectiveFrom?: string;
}
export interface Permissions {
	roles: string[];
	operations: { method: string; path: string; access: string; roles: string[] }[];
}
export interface Evidence {
	quote: string;
	found: boolean;
}
export interface Suggestion {
	id: string;
	kind: string;
	title: string;
	why: string;
	confidence: 'high' | 'medium' | 'low';
	evidence: Evidence[];
	data: Record<string, any>;
	notes?: string[];
}
export interface AssistResult {
	answer: string;
	suggestions: Suggestion[];
	questions: string[];
	provider: string;
	model: string;
	usage: { promptTokens: number; completionTokens: number };
	aliased: number;
	truncated: boolean;
}
export interface AssistantStatus {
	available: boolean;
	provider?: string;
	model?: string;
	reason?: string;
	external?: boolean;
	budget?: { dailyRequests: number; usedToday: number };
}
export interface AssignResult {
	class?: string;
	mappedBy?: string;
	allowed?: boolean;
	assignment?: { rule: string; account: string; why: string[]; considered: { rule: string; account: string; matched: boolean; reason: string; priority: number }[] };
	error?: { code: string; message: string };
	gate?: { code: string; message: string };
}
export interface Decision {
	id: string;
	area: string;
	step: string;
	configId: string;
	version: number;
	suggestionId: string;
	title: string;
	decision: 'ACCEPTED' | 'EDITED' | 'REJECTED';
	detail: unknown;
	actor: string;
	at: string;
}
export type Area = 'admin' | 'onboarding' | 'stage1';

export function wsClient(tenant: string, fetcher: Fetcher = (i, o) => fetch(i, o)) {
	async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
		const headers: Record<string, string> = { 'x-gl-tenant': tenant, accept: 'application/json' };
		if (body !== undefined) headers['content-type'] = 'application/json';
		const res = await fetcher(BASE + path, { method, headers, credentials: 'same-origin', body: body === undefined ? undefined : JSON.stringify(body) });
		if (!res.ok) throw await toError(res);
		return (await res.json()) as T;
	}
	const v = (id: string, n: number) => `/configs/${encodeURIComponent(id)}/versions/${n}`;
	return {
		...client(tenant, fetcher),
		tenant,
		library: () => call<ClassLibrary>('GET', '/catalog/classes'),
		permissions: () => call<Permissions>('GET', '/roles/permissions'),
		getClient: () => call<ClientProfile>('GET', '/client'),
		putClient: (body: { name: string; status?: string; configId?: string; profile: OnboardingProfile; reason: string; initialize?: { title: string; effectiveFrom: string } }) =>
			call<{ client: ClientProfile; initialized?: string }>('PUT', '/client', body),
		clientAudit: () => call<{ entries: { id: string; actor: string; action: string; reason: string; at: string }[] }>('GET', '/client/audit').then((r) => r.entries),
		assign: (id: string, n: number, body: { class?: string; sourceSystem?: string; sourceClass?: string; conditions: Record<string, string>; entity?: string; currency?: string; bookDate: string }) =>
			call<AssignResult>('POST', v(id, n) + '/assign', body),
		returnToDraft: (id: string, n: number, reason: string) => call<VersionFull>('POST', v(id, n) + '/return', { reason }),
		assistantStatus: () => call<AssistantStatus>('GET', '/ai/status'),
		assist: (body: { area: Area; step?: string; configId?: string; version?: number; question: string; documents?: { name: string; text: string }[]; selection?: unknown }) =>
			call<{ result: AssistResult; budget: { dailyRequests: number; usedToday: number } }>('POST', '/ai/assist', body),
		decide: (body: { area: Area; step?: string; configId?: string; version?: number; suggestionId: string; title: string; decision: 'ACCEPTED' | 'EDITED' | 'REJECTED'; detail?: unknown; provider?: string; model?: string }) =>
			call<{ recorded: boolean }>('POST', '/ai/decisions', body),
		decisions: (q: { area?: Area; configId?: string } = {}) =>
			call<{ decisions: Decision[] }>('GET', '/ai/decisions?' + new URLSearchParams(q as Record<string, string>).toString()).then((r) => r.decisions),
		checkContent: (id: string, n: number, content: unknown) =>
			call<{ ok: boolean; findings: Finding[]; contentDigest: string }>('POST', v(id, n) + '/check', { content })
	};
}

export type WsClient = ReturnType<typeof wsClient>;

export const roleLabels: Record<string, string> = {
	GL_ADMIN: 'Administrator',
	CONFIG_EDITOR: 'Business analyst',
	CONFIG_APPROVER: 'Reviewer / approver',
	OPERATOR: 'Operator',
	VIEWER: 'Viewer / finance'
};
export const allRoles = Object.keys(roleLabels);
