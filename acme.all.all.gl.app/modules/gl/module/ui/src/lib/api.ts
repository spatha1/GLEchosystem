// Typed client for the /m/gl API. The element sends the tenant (x-gl-tenant) and never an
// identity: the app core authenticates the user and sets x-mycel-user.
import type { AuditEntry, ConfigSummary, Me, PostingPlan, Version } from './types';

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
	const v = (id: string, n: number) => `/configs/${encodeURIComponent(id)}/versions/${n}`;
	return {
		me: () => call<Me>('GET', '/me'),
		configs: () => call<{ configs: ConfigSummary[] }>('GET', '/configs').then((r) => r.configs),
		versions: (id: string) => call<{ versions: Version[] }>('GET', `/configs/${encodeURIComponent(id)}/versions`).then((r) => r.versions),
		version: (id: string, n: number) => call<Version>('GET', v(id, n)),
		validate: (id: string, n: number) => call<Version>('POST', v(id, n) + '/validate'),
		approve: (id: string, n: number, reason: string) => call<Version>('POST', v(id, n) + '/approve', { reason }),
		retire: (id: string, n: number, reason: string) => call<Version>('POST', v(id, n) + '/retire', { reason }),
		audit: (id: string) => call<{ entries: AuditEntry[] }>('GET', `/configs/${encodeURIComponent(id)}/audit`).then((r) => r.entries),
		plan: (id: string, n: number, event: unknown) => call<{ plan: PostingPlan }>('POST', v(id, n) + '/plan', event).then((r) => r.plan)
	};
}

export type Client = ReturnType<typeof client>;
