// Studio tests. The event views and the plan are recorded from the real API over the
// GL-TEST-001 configuration (src/test/*.json), so the screens are tested on what the server
// actually returns.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { Ctx } from '../lib/ctx';
import type { Content, EventView, Plan, Stage } from '../lib/studio';
import { fromHash, toHash, type Route } from '../lib/route';
import { cleanRule, uniqueCode, withRule, withoutRule } from '../lib/content';
import { accountText, entityText } from '../lib/explain';
import { connected } from '../lib/lineage';
import premRaw from '../test/prem-added.view.json';
import payRaw from '../test/payment-received.view.json';
import e3Raw from '../test/e3.plan.json';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';
import RulePage from './RulePage.svelte';
import FieldsTable from './FieldsTable.svelte';
import Simulator from './Simulator.svelte';
import Pipeline from './Pipeline.svelte';
import LineageMap from './LineageMap.svelte';

const prem = (premRaw as unknown as { view: EventView }).view;
const pay = (payRaw as unknown as { view: EventView }).view;
const e3 = (e3Raw as unknown as { plan: Plan }).plan;
const content = structuredClone(fixtureConfig) as unknown as Content;

beforeAll(() => {
	(globalThis as any).ResizeObserver ??= class {
		observe() {}
		disconnect() {}
	};
});
afterEach(cleanup);

function ctxFor(over: Partial<Ctx> & { route: Route }): Ctx {
	const api: any = {
		plan: vi.fn(async () => e3),
		runScenarios: vi.fn(async () => ({ summary: { PASSED: 1 }, results: [{ code: 'E3', eventType: 'PAYMENT_RECEIVED', status: 'PASSED', mismatches: [] }] })),
		check: vi.fn(async () => ({ ok: true, findings: [], contentDigest: 'sha256:x', changes: [] }))
	};
	return {
		api,
		me: { tenantId: 'CLIENT_A', actor: 'ba@example.com', roles: ['CONFIG_EDITOR'] },
		version: {
			tenantId: 'CLIENT_A',
			configId: 'GL_TEST_001',
			version: 1,
			status: 'approved',
			effectiveFrom: '2026-01-01',
			effectiveTo: null,
			contentDigest: 'sha256:abc',
			createdBy: 'ba@example.com',
			createdAt: '',
			updatedBy: '',
			updatedAt: '',
			editors: ['ba@example.com']
		},
		content,
		canEdit: false,
		isEditor: true,
		go: vi.fn(),
		save: vi.fn(async () => {}),
		startDraft: vi.fn(),
		tick: 0,
		...over
	};
}

describe('routes', () => {
	it('round-trip through the URL, so every view can be linked to', () => {
		const cases: Route[] = [
			{},
			{ configId: 'GL_TEST_001', version: 1 },
			{ configId: 'GL_TEST_001', version: 2, tab: 'impact' },
			{ configId: 'GL_TEST_001', version: 1, event: 'PREM_ADDED', etab: 'lineage', select: 'field:PREMIUM' },
			{ configId: 'GL_TEST_001', version: 2, event: 'PREM_ADDED', rule: 'PR_PREM_ADDED', edit: true },
			{ configId: 'GL_TEST_001', version: 1, event: 'PAYMENT_RECEIVED', etab: 'simulate', scenario: 'E3' }
		];
		for (const r of cases) expect(fromHash(toHash(r))).toEqual(r);
	});
});

describe('editing content', () => {
	it('replaces a rule by its code, appends a new one, and stores only meaningful fields', () => {
		const rule = { ...content.postingRules[0], filter: '  ', active: true, description: ' Premium ' };
		const c = withRule(content, rule, 'PR_PREM_ADDED');
		expect(c.postingRules).toHaveLength(content.postingRules.length);
		expect(c.postingRules[0]).not.toHaveProperty('filter');
		expect(c.postingRules[0]).not.toHaveProperty('active');
		expect(c.postingRules[0].description).toBe('Premium');
		const off = cleanRule({ ...rule, active: false });
		expect(off.active).toBe(false);
		const added = withRule(content, { ...rule, code: 'PR_NEW' });
		expect(added.postingRules.map((r: any) => r.code)).toContain('PR_NEW');
		expect(withoutRule(content, 'PR_INVOICED').postingRules.map((r: any) => r.code)).not.toContain('PR_INVOICED');
		expect(content.postingRules[0].lines[0].amount).toBe('GROSS'); // the stored content is never mutated
		expect(uniqueCode('PR_PREM_ADDED_COPY', ['PR_PREM_ADDED_COPY'])).toBe('PR_PREM_ADDED_COPY_2');
	});
});

describe('explanations come from the configuration', () => {
	it('words the AR0 account choice in precedence order', () => {
		const ar0 = prem.rules[0].lines.find((l) => l.line.code === 'AR0_DR')!;
		const t = accountText(ar0);
		expect(t[0]).toMatch(/^AA_AR0_MOTOR: 9948523 .* when LOB = MOTOR$/);
		expect(t[1]).toMatch(/^AA_AR0_DEFAULT: 9948500 .* otherwise$/);
	});
	it('says the cash leg posts in the banking company derived from the bank account', () => {
		const cash = pay.rules[0].lines.find((l) => l.line.code === 'CASH')!;
		expect(entityText(pay, cash)).toBe('BANKING_COMPANY, derived by lookup BANKING_COMPANY_BY_ACCOUNT from BANK_ACCOUNT');
	});
	it('traces PREMIUM through GROSS to the lines, classes and accounts it reaches, and nothing else', () => {
		const lit = connected(prem.graph, 'field:PREMIUM');
		for (const id of ['derived:GROSS', 'line:PR_PREM_ADDED/AR0_DR', 'line:PR_PREM_ADDED/PW0_CR', 'class:AR0', 'account:9948523', 'account:4100100'])
			expect(lit.has(id), id).toBe(true);
		expect(lit.has('line:PR_PREM_ADDED/TX0_CR')).toBe(false);
		expect(lit.has('field:TAX')).toBe(false);
	});
});

describe('rule page', () => {
	it('explains every line of an approved rule and offers a draft, never an edit', () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 1, event: 'PREM_ADDED', rule: 'PR_PREM_ADDED' } });
		render(RulePage, { ctx, view: prem });
		expect(screen.getByRole('heading', { name: 'PR_PREM_ADDED' })).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Edit rule' })).toBeNull();
		expect(screen.getByRole('button', { name: 'Change this rule in a new draft' })).toBeTruthy();
		const lines = screen.getByRole('heading', { name: /Debit and credit lines/ }).closest('section')!;
		expect(within(lines).getAllByText('Debit')).toHaveLength(1);
		expect(within(lines).getAllByText('Credit')).toHaveLength(2);
		expect(within(lines).getByText(/AA_AR0_MOTOR: 9948523 .* when LOB = MOTOR/)).toBeTruthy();
	});
	it('in a draft, deactivating saves the rule as inactive with a reason', async () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 2, event: 'PREM_ADDED', rule: 'PR_PREM_ADDED' }, canEdit: true });
		ctx.version = { ...ctx.version, version: 2, status: 'draft' };
		render(RulePage, { ctx, view: prem });
		await fireEvent.click(screen.getByRole('button', { name: 'Deactivate' }));
		const dialog = screen.getByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'replaced by a dated rule' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Deactivate' }));
		expect(ctx.save).toHaveBeenCalledTimes(1);
		const [saved, reason] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('replaced by a dated rule');
		expect(saved.postingRules.find((r: any) => r.code === 'PR_PREM_ADDED').active).toBe(false);
	});
});

describe('required fields', () => {
	it('lists the bank account as mandatory because of the lookup that sets the cash entity, and never asks for the banking company', async () => {
		render(FieldsTable, { explanation: pay.explanation, manifest: pay.manifest });
		const row = screen.getByText('BANK_ACCOUNT').closest('tr')!;
		expect(within(row).getByText('mandatory')).toBeTruthy();
		await fireEvent.click(within(row).getByRole('button', { name: 'Why' }));
		expect(screen.getAllByText(/via BANKING_COMPANY = lookup BANKING_COMPANY_BY_ACCOUNT\(BANK_ACCOUNT\)/).length).toBeGreaterThan(0);
		const calc = screen.getByRole('heading', { name: /Calculated by GL/ }).closest('section')!;
		expect(within(calc).getByText('BANKING_COMPANY')).toBeTruthy();
		const collect = screen.getByRole('heading', { name: /Fields to collect/ }).closest('section')!;
		expect(within(collect).queryByText('BANKING_COMPANY')).toBeNull();
	});
	it('reports a declared field nothing reads as unused', () => {
		render(FieldsTable, { explanation: prem.explanation, manifest: prem.manifest });
		const unused = screen.getByRole('heading', { name: 'Declared but not used' }).closest('section')!;
		expect(within(unused).getByText('PRODUCT')).toBeTruthy();
	});
});

describe('simulation', () => {
	it('runs a scenario of the configuration and traces a journal line back to its source field and rule', async () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 1, event: 'PAYMENT_RECEIVED', etab: 'simulate', scenario: 'E3' } });
		render(Simulator, { ctx, fixedEvent: pay });
		await vi.waitFor(() => expect(screen.getByRole('heading', { name: /^Journal/ })).toBeTruthy());
		expect((ctx.api.plan as any).mock.calls[0][2].eventType).toBe('PAYMENT_RECEIVED');
		expect((ctx.api.plan as any).mock.calls[0][2].lists.ALLOCATION_LIST).toHaveLength(2);
		const journal = screen.getByRole('heading', { name: /^Journal/ }).closest('section')!;
		expect(within(journal).getAllByText(/balanced · Dr 1,099.50 · Cr 1,099.50/)).toHaveLength(2); // C09 and C01, each balanced
		await vi.waitFor(() => expect(screen.getByText('Matches expectation')).toBeTruthy());
		const alloc = within(journal).getByText('550.00').closest('tr')!;
		await fireEvent.click(alloc);
		const lineage = screen.getByRole('heading', { name: /Where line 4 comes from/ }).closest('section')!;
		expect(within(lineage).getByRole('cell', { name: 'ALLOC_AMT' })).toBeTruthy();
		expect(within(lineage).getByRole('cell', { name: /^550(\.00)? AUD$/ })).toBeTruthy();
		expect(within(lineage).getByText('lists.ALLOCATION_LIST[].measures.ALLOC_AMT')).toBeTruthy();
		expect(within(lineage).getByText('AA_AR1')).toBeTruthy();
		expect(within(lineage).getByRole('button', { name: 'PR_PAYMENT_RECEIVED' })).toBeTruthy();
	});
});

describe('pipeline', () => {
	it('shows stages that are not built as such', async () => {
		const stages: Stage[] = [
			{ stage: 'approved', label: 'Approved', status: 'DONE', detail: 'approved by reviewer@' },
			{ stage: 'mapped', label: 'Data Workbench mapping', status: 'NOT_IMPLEMENTED', detail: 'not built', story: 'US-015' }
		];
		const onselect = vi.fn();
		render(Pipeline, { stages, onselect });
		const mapped = screen.getByRole('button', { name: /Data Workbench mapping/ });
		expect(within(mapped).getByText('Not built yet')).toBeTruthy();
		await fireEvent.click(mapped);
		expect(onselect).toHaveBeenCalledWith(stages[1]);
	});
});

describe('lineage map', () => {
	it('shows what a selected field is used by', () => {
		render(LineageMap, { graph: prem.graph, selected: 'field:PREMIUM', onselect: vi.fn() });
		const panel = screen.getByRole('heading', { name: 'PREMIUM' }).closest('aside')!;
		expect(within(panel).getByRole('button', { name: 'GROSS' })).toBeTruthy();
		expect(within(panel).getByRole('button', { name: 'PR_PREM_ADDED · PW0_CR' })).toBeTruthy();
	});
});
