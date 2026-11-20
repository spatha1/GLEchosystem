// Typed client for the /m/gl API. The element sends the tenant (x-gl-tenant) and never an
// identity: the app core authenticates the user and sets x-mycel-user.
import type { ConfigSummary, Finding, Me } from './types';
import type {
	AuditEntryFull,
	EventSummary,
	EventView,
	Impact,
	ItemChange,
	ManifestView,
	Plan,
	ScenarioResult,
	Stage,
	VersionFull
} from './studio';

export const BASE = '/m/gl';

/** The API's error envelope {error:{code,message,field,retryable,detail}}. */
export class ApiError extends Error {
	constructor(
		public readonly status: number,
		public readonly code: string,
		message: string,
		public readonly field = '',
		public readonly detail: unknown = undefined
	) {
		super(message);
		this.name = 'ApiError';
	}
}

export async function toError(res: Response): Promise<ApiError> {
	let body: any = null;
	try {
		body = await res.json();
	} catch {
		/* not JSON */
	}
	const e = body?.error;
	if (e && typeof e === 'object') {
		return new ApiError(res.status, String(e.code ?? 'ERROR'), String(e.message ?? res.statusText), typeof e.field === 'string' ? e.field : '', e.detail);
	}
	return new ApiError(res.status, res.status === 401 ? 'UNAUTHENTICATED' : 'ERROR', res.statusText || 'Request failed');
}

export type Fetcher = (input: string, init?: RequestInit) => Promise<Response>;

export function client(tenant: string, fetcher: Fetcher = (i, o) => fetch(i, o)) {
	async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
		const headers: Record<string, string> = { 'x-gl-tenant': tenant, accept: 'application/json' };
		if (body !== undefined) headers['content-type'] = 'application/json';
		const res = await fetcher(BASE + path, {
			method,
			headers,
			credentials: 'same-origin',
			body: body === undefined ? undefined : JSON.stringify(body)
		});
		if (!res.ok) throw await toError(res);
		return (await res.json()) as T;
	}
	const c = (id: string) => `/configs/${encodeURIComponent(id)}`;
	const v = (id: string, n: number) => `${c(id)}/versions/${n}`;
	return {
		me: () => call<Me>('GET', '/me'),
		configs: () => call<{ configs: ConfigSummary[] }>('GET', '/configs').then((r) => r.configs),
		versions: (id: string) => call<{ versions: VersionFull[] }>('GET', c(id) + '/versions').then((r) => r.versions),
		version: (id: string, n: number) => call<VersionFull>('GET', v(id, n)),
		validate: (id: string, n: number) => call<VersionFull>('POST', v(id, n) + '/validate'),
		approve: (id: string, n: number, reason: string) => call<VersionFull>('POST', v(id, n) + '/approve', { reason }),
		retire: (id: string, n: number, reason: string) => call<VersionFull>('POST', v(id, n) + '/retire', { reason }),
		audit: (id: string) => call<{ entries: AuditEntryFull[] }>('GET', c(id) + '/audit').then((r) => r.entries),
		plan: (id: string, n: number, event: unknown) => call<{ plan: Plan }>('POST', v(id, n) + '/plan', event).then((r) => r.plan),
		/** A new draft copied from another version. */
		newDraft: (id: string, basedOn: number, effectiveFrom: string, reason: string) =>
			call<VersionFull>('POST', c(id) + '/versions', { basedOnVersion: basedOn, effectiveFrom, reason }),
		/** Save edited content to a draft; baseDigest guards against overwriting another editor. */
		saveDraft: (id: string, n: number, baseDigest: string, content: unknown, reason: string) =>
			call<VersionFull>('PUT', v(id, n), { baseDigest, content, reason }),
		/** Validate edited content without saving it. */
		check: (id: string, n: number, content: unknown) =>
			call<{ ok: boolean; findings: Finding[]; contentDigest: string; changes: ItemChange[] }>('POST', v(id, n) + '/check', { content }),
		events: (id: string, n: number) => call<{ events: EventSummary[]; blockingFindings: number; findings: Finding[] }>('GET', v(id, n) + '/events'),
		event: (id: string, n: number, eventType: string) =>
			call<{ view: EventView }>('GET', v(id, n) + '/events/' + encodeURIComponent(eventType)).then((r) => r.view),
		manifest: (id: string, n: number) => call<ManifestView>('GET', v(id, n) + '/manifest'),
		runScenarios: (id: string, n: number, codes?: string[]) =>
			call<{ summary: Record<string, number>; results: ScenarioResult[] }>('POST', v(id, n) + '/scenarios/run', codes ? { codes } : {}),
		impact: (id: string, n: number, against?: number) => call<Impact>('GET', v(id, n) + '/impact' + (against ? `?against=${against}` : '')),
		pipeline: (id: string, n: number) => call<{ stages: Stage[] }>('GET', v(id, n) + '/pipeline').then((r) => r.stages)
	};
}

export type Client = ReturnType<typeof client>;
