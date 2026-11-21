// Where the studio is: client → configuration → version → event → rule. The route is plain
// data; the element keeps it in its state and, when asked to (hash-routing), mirrors it in
// the URL so a view can be linked to and the browser's back button works.

export type VersionTab = 'events' | 'ai' | 'fields' | 'mapping' | 'simulate' | 'impact' | 'accounts' | 'history';
/** The steps of an event's guided flow (plus the lineage map). */
export type EventTab =
	| 'details'
	| 'measures'
	| 'context'
	| 'rules'
	| 'lines'
	| 'fields'
	| 'mapping'
	| 'collection'
	| 'simulate'
	| 'journal'
	| 'subledger'
	| 'reconciliation'
	| 'reports'
	| 'lineage'
	| 'overview';

export interface Route {
	configId?: string;
	version?: number;
	tab?: VersionTab;
	event?: string;
	etab?: EventTab;
	rule?: string; // a rule code, or "+new"
	edit?: boolean;
	scenario?: string;
	select?: string; // a lineage node to select
}

const versionTabs: VersionTab[] = ['events', 'ai', 'fields', 'mapping', 'simulate', 'impact', 'accounts', 'history'];
const eventTabs: EventTab[] = [
	'details',
	'measures',
	'context',
	'rules',
	'lines',
	'fields',
	'mapping',
	'collection',
	'simulate',
	'journal',
	'subledger',
	'reconciliation',
	'reports',
	'lineage',
	'overview'
];

export function toHash(r: Route): string {
	let p = '#/';
	const q = new URLSearchParams();
	if (r.configId) {
		p += `c/${encodeURIComponent(r.configId)}`;
		if (r.version) {
			p += `/v/${r.version}`;
			if (r.event) {
				p += `/e/${encodeURIComponent(r.event)}`;
				if (r.rule) p += `/r/${encodeURIComponent(r.rule)}`;
				else if (r.etab && r.etab !== 'overview' && r.etab !== 'details') q.set('tab', r.etab);
			} else if (r.tab && r.tab !== 'events') {
				q.set('tab', r.tab);
			}
		}
	}
	if (r.edit) q.set('edit', '1');
	if (r.scenario) q.set('scenario', r.scenario);
	if (r.select) q.set('select', r.select);
	const qs = q.toString();
	return qs ? `${p}?${qs}` : p;
}

export function fromHash(hash: string): Route {
	const [path, query = ''] = hash.replace(/^#\/?/, '').split('?');
	const seg = path.split('/').filter(Boolean).map(decodeURIComponent);
	const q = new URLSearchParams(query);
	const r: Route = {};
	for (let i = 0; i + 1 < seg.length; i += 2) {
		const [k, v] = [seg[i], seg[i + 1]];
		if (k === 'c') r.configId = v;
		if (k === 'v' && /^\d+$/.test(v)) r.version = Number(v);
		if (k === 'e') r.event = v;
		if (k === 'r') r.rule = v;
	}
	const tab = q.get('tab') ?? '';
	if (r.event) {
		if (eventTabs.includes(tab as EventTab)) r.etab = tab as EventTab;
	} else if (versionTabs.includes(tab as VersionTab)) {
		r.tab = tab as VersionTab;
	}
	if (q.get('edit') === '1') r.edit = true;
	if (q.get('scenario')) r.scenario = q.get('scenario')!;
	if (q.get('select')) r.select = q.get('select')!;
	return r;
}
