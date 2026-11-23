// Where the workspace is. Plain data, mirrored in the URL hash so every page can be linked to:
//   #/            home (administration for administrators, else the client list)
//   #/admin/C     administration of client C
//   #/onboard/C   onboarding client C
//   #/client/C    client C's workspace
//   #/client/C/stage1/3   Stage 1, step 3
export type Page = 'home' | 'admin' | 'onboard' | 'client' | 'stage1';

export interface WsRoute {
	page: Page;
	client?: string;
	step?: number;
}

export const STAGE1_STEPS = [
	{ n: 1, key: 'import', title: 'Import COA', hint: 'The client’s chart of accounts and class list' },
	{ n: 2, key: 'classes', title: 'Map accounting classes', hint: 'Source class codes → standard classes' },
	{ n: 3, key: 'assignments', title: 'Account assignment rules', hint: 'Class + conditions → account' },
	{ n: 4, key: 'review', title: 'Review & validate', hint: 'Findings, coverage, rule testing' },
	{ n: 5, key: 'submit', title: 'Save & submit', hint: 'Submit for independent review' }
] as const;

const code = /^[A-Z][A-Z0-9_]*$/;

export function parseHash(hash: string): WsRoute {
	const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent);
	const c = parts[1] && code.test(parts[1]) ? parts[1] : undefined;
	switch (parts[0]) {
		case 'admin':
			return { page: 'admin', client: c };
		case 'onboard':
			return { page: 'onboard', client: c };
		case 'client':
			if (!c) return { page: 'home' };
			if (parts[2] === 'stage1') {
				const n = Number(parts[3]);
				return { page: 'stage1', client: c, step: n >= 1 && n <= 5 ? n : 1 };
			}
			return { page: 'client', client: c };
	}
	return { page: 'home' };
}

export function toHash(r: WsRoute): string {
	const c = r.client ? '/' + encodeURIComponent(r.client) : '';
	switch (r.page) {
		case 'admin':
			return '#/admin' + c;
		case 'onboard':
			return '#/onboard' + c;
		case 'client':
			return '#/client' + c;
		case 'stage1':
			return `#/client${c}/stage1/${r.step ?? 1}`;
	}
	return '#/';
}

/** Clients from the host: "CODE:Label,CODE2:Label 2". */
export function parseClients(s: string): { code: string; label: string }[] {
	return String(s ?? '')
		.split(',')
		.map((x) => x.trim())
		.filter(Boolean)
		.map((x) => {
			const [c, ...l] = x.split(':');
			return { code: c.trim(), label: (l.join(':') || c).trim() };
		})
		.filter((x) => code.test(x.code));
}
