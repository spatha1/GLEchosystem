import { describe, expect, it } from 'vitest';
import { ApiError, client, toError } from './api';
import { amount, digest, keyList, when } from './format';
import { actionsFor } from './permissions';
import type { Me, Version } from './types';
import { specs, type Catalog, type Field, type Option } from './sections';
import { movementOf, movementText, movementWord } from './explain';

describe('format', () => {
	it('regroups exact decimals without floating point', () => {
		expect(amount('1099.50')).toBe('1,099.50');
		expect(amount('1234567.005')).toBe('1,234,567.005');
		expect(amount('-20.00')).toBe('-20.00');
		expect(amount('0.10')).toBe('0.10');
		expect(amount('12345678901234567890.01')).toBe('12,345,678,901,234,567,890.01');
		expect(amount('n/a')).toBe('n/a');
	});
	it('shortens timestamps, digests and keys', () => {
		expect(when('2026-01-10T09:15:02.123Z')).toBe('2026-01-10 09:15');
		expect(digest('sha256:0123456789abcdef')).toBe('01234567');
		expect(keyList({ RECEIPT_NO: 'R1', INVOICE_NO: 'INV1' })).toBe('INVOICE_NO INV1 · RECEIPT_NO R1');
	});
});

describe('api client', () => {
	it('sends the tenant and never an identity', async () => {
		let seen: RequestInit | undefined;
		let url = '';
		const api = client('DEMO', async (u, init) => {
			url = u;
			seen = init;
			return new Response(JSON.stringify({ plan: { lines: [] } }), { status: 200 });
		});
		await api.plan('DEMO_GL', 2, { eventType: 'PREM_ADDED' });
		const h = seen!.headers as Record<string, string>;
		expect(url).toBe('/m/gl/configs/DEMO_GL/versions/2/plan');
		expect(seen!.method).toBe('POST');
		expect(h['x-gl-tenant']).toBe('DEMO');
		expect(Object.keys(h).map((k) => k.toLowerCase())).not.toContain('x-mycel-user');
		expect(JSON.parse(seen!.body as string)).toEqual({ eventType: 'PREM_ADDED' });
	});
	it('turns the error envelope into an ApiError with its detail', async () => {
		const res = new Response(JSON.stringify({ error: { code: 'UNBALANCED', message: 'off by 1', detail: { lines: [1] } } }), { status: 422 });
		const e = await toError(res);
		expect(e).toBeInstanceOf(ApiError);
		expect([e.status, e.code, e.message]).toEqual([422, 'UNBALANCED', 'off by 1']);
		expect(e.detail).toEqual({ lines: [1] });
		const api = client('DEMO', async () => new Response(JSON.stringify({ error: { code: 'SELF_APPROVAL', message: 'another approver must' } }), { status: 403 }));
		await expect(api.approve('DEMO_GL', 2, 'ok')).rejects.toMatchObject({ code: 'SELF_APPROVAL', status: 403 });
	});
	it('reports a non-JSON failure', async () => {
		const e = await toError(new Response('bad gateway', { status: 502, statusText: 'Bad Gateway' }));
		expect([e.status, e.code, e.message]).toEqual([502, 'ERROR', 'Bad Gateway']);
	});
});

const version = (status: Version['status'], editors: string[]): Version => ({
	tenantId: 'DEMO',
	configId: 'DEMO_GL',
	version: 2,
	status,
	effectiveFrom: '2026-02-01',
	effectiveTo: null,
	contentDigest: 'sha256:00',
	createdBy: editors[0],
	createdAt: '',
	updatedBy: editors[0],
	updatedAt: '',
	editors
});
const me = (actor: string, ...roles: Me['roles']): Me => ({ tenantId: 'DEMO', actor, roles });

describe('maker-checker on screen', () => {
	it('lets an approver approve what someone else changed', () => {
		const a = actionsFor(version('validated', ['ba@example.com']), me('reviewer@example.com', 'CONFIG_APPROVER'));
		expect(a.find((x) => x.kind === 'approve')).toMatchObject({ enabled: true });
	});
	it('explains why an editor-approver cannot approve their own change', () => {
		const a = actionsFor(version('validated', ['lead@example.com']), me('lead@example.com', 'CONFIG_EDITOR', 'CONFIG_APPROVER'));
		const approve = a.find((x) => x.kind === 'approve')!;
		expect(approve.enabled).toBe(false);
		expect(approve.why).toMatch(/maker-checker/);
	});
	it('offers each action only in its state and only with its role', () => {
		expect(actionsFor(version('draft', ['ba@example.com']), me('ba@example.com', 'CONFIG_EDITOR')).map((x) => [x.kind, x.enabled])).toEqual([['validate', true]]);
		expect(actionsFor(version('draft', ['ba@example.com']), me('v@example.com', 'VIEWER'))[0]).toMatchObject({ kind: 'validate', enabled: false });
		expect(actionsFor(version('approved', ['ba@example.com']), me('r@example.com', 'CONFIG_APPROVER')).map((x) => x.kind)).toEqual(['retire']);
		expect(actionsFor(version('retired', ['ba@example.com']), me('r@example.com', 'CONFIG_APPROVER'))).toEqual([]);
	});
});

describe('section editors', () => {
	// A list offering a value twice breaks the form (keyed options), so every list of every editor is checked
	// on a configuration whose values overlap the standard catalog (CURRENCY is both).
	it('offers each choice once in every list of every editor', () => {
		const c = {
			sourceSystems: [{ code: 'POLICY', kind: 'POLICY' }],
			events: [{ code: 'PREM_ADDED', economicKey: ['POLICY_NO'], measures: ['PREMIUM'], dimensions: ['LOB', 'CURRENCY'] }],
			baseMeasures: [{ code: 'BROKER_FEE' }],
			derivedMeasures: [{ code: 'GROSS', formula: 'PREMIUM' }],
			dimensions: [{ code: 'LOB' }, { code: 'CURRENCY' }, { code: 'COMPANY' }],
			lookups: [{ code: 'COMPANY_BY_LOB' }],
			keyProfiles: [{ code: 'POLICY_NO' }],
			calendars: [{ code: 'AU' }],
			datePolicies: [{ code: 'DP' }],
			accountingClasses: [{ code: 'AR0', name: 'Receivable' }],
			accounts: [{ code: '9948500', name: 'Receivable', class: 'AR0' }]
		};
		const cat: Catalog = {
			measures: [{ value: 'PREMIUM', label: 'PREMIUM' }],
			dimensions: ['COMPANY', 'LOB', 'CURRENCY'].map((value) => ({ value, label: value }))
		};
		const repeated = (xs: Option[]) => xs.map((x) => x.value).filter((v, i, all) => all.indexOf(v) !== i);
		const found: string[] = [];
		const walk = (where: string, fields: Field[]) => {
			for (const f of fields) {
				for (const [kind, list] of [
					['options', f.options?.(c, cat, {})],
					['pairKeys', f.pairKeys?.(c, cat, {})]
				] as const) {
					const dup = list ? repeated(list) : [];
					if (dup.length) found.push(`${where}.${f.key} ${kind}: ${dup.join(', ')}`);
				}
				if (f.fields) walk(`${where}.${f.key}`, f.fields);
			}
		};
		for (const [name, spec] of Object.entries(specs)) walk(name, spec.fields);
		expect(found).toEqual([]);
		// ALTB-03: an assignment rule can also match on the event type.
		const match = specs.accountAssignmentRules.fields.find((f) => f.key === 'match')!;
		expect(match.pairKeys!(c, cat, {}).map((o) => o.value)).toEqual(expect.arrayContaining(['ENTITY', 'CURRENCY', 'EVENT_TYPE', 'LOB']));
	});
});

describe('movement (ALTB-01)', () => {
	it("reads a side against the class's normal side, so a contra class reads correctly", () => {
		expect(movementOf('D', 'D')).toBe('INCREASE');
		expect(movementOf('C', 'D')).toBe('DECREASE');
		expect(movementOf('D', 'C')).toBe('DECREASE');
		expect(movementOf('D', undefined)).toBeUndefined();
		expect(movementWord('INCREASE')).toBe('▲ increases');
		expect(movementWord('DECREASE')).toBe('▼ decreases');
		expect(movementText('D', { name: 'Provision for doubtful premium', nature: 'ASSET', normalSide: 'C' })).toBe(
			'A positive amount decreases Provision for doubtful premium (asset, normally a credit); a negative amount posts on the other side and increases it.'
		);
	});
});
