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
import eventCatalog from '../../../service/model/ootb_events.json';
import RulePage from './RulePage.svelte';
import FieldsTable from './FieldsTable.svelte';
import Simulator from './Simulator.svelte';
import Pipeline from './Pipeline.svelte';
import LineageMap from './LineageMap.svelte';
import Home from './Home.svelte';
import SectionEditor from './SectionEditor.svelte';
import EventPage from './EventPage.svelte';
import { cleanItem, specs, withItem } from '../lib/sections';

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
		catalog: { measures: [{ value: 'PREMIUM', label: 'PREMIUM · Premium' }, { value: 'FEE', label: 'FEE · Fee' }], dimensions: [{ value: 'LOB', label: 'LOB' }] },
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
			{ configId: 'GL_TEST_001', version: 1, event: 'PAYMENT_RECEIVED', etab: 'simulate', scenario: 'E3' },
			{ configId: 'GL_TEST_001', version: 1, event: 'PREM_ADDED', etab: 'journal' }
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

describe('saving a test case', () => {
	it('saves a case started from a blank event under a new code, never over an existing scenario', async () => {
		const existing = { code: 'PAYMENT_RECEIVED_SIM', eventType: 'PAYMENT_RECEIVED', input: { currency: 'AUD' }, expect: { refused: 'NO_RULE' } };
		const ctx = ctxFor({
			route: { configId: 'GL_TEST_001', version: 1, event: 'PAYMENT_RECEIVED', etab: 'simulate' },
			canEdit: true,
			content: { ...content, scenarios: [...((content.scenarios ?? []) as any[]), existing] }
		});
		render(Simulator, { ctx, fixedEvent: pay });
		const start = await screen.findByRole('combobox', { name: /Start from/ });
		await fireEvent.change(start, { target: { value: '' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Run simulation' }));
		await fireEvent.click(await screen.findByRole('button', { name: 'Save as scenario in draft' }));
		const dialog = await screen.findByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'A second case' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalled());
		const scenarios = (ctx.save as any).mock.calls[0][0].scenarios as { code: string; expect: unknown }[];
		expect(scenarios.find((s) => s.code === 'PAYMENT_RECEIVED_SIM')?.expect).toEqual({ refused: 'NO_RULE' });
		expect(scenarios.some((s) => s.code === 'PAYMENT_RECEIVED_SIM_2')).toBe(true);
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

describe('landing page', () => {
	const v = (version: number, status: string, extra = {}) => ({
		tenantId: 'CLIENT_A', configId: 'GL_TEST_001', version, status, effectiveFrom: '2026-01-01', effectiveTo: null,
		contentDigest: 'sha256:abc', createdBy: 'ba@example.com', createdAt: '', updatedBy: '', updatedAt: '', editors: ['ba@example.com'], ...extra
	});
	function homeApi() {
		return {
			configs: vi.fn(async () => [{ configId: 'GL_TEST_001', title: 'GL-TEST-001', createdBy: 'ba@example.com', createdAt: '', latestVersion: 2 }]),
			versions: vi.fn(async () => [v(1, 'approved'), v(2, 'draft', { basedOnVersion: 1 })]),
			version: vi.fn(async () => ({ ...v(1, 'approved'), content })),
			createConfig: vi.fn(async (configId: string) => ({ ...v(1, 'draft'), configId })),
			newDraft: vi.fn(async () => v(3, 'draft'))
		};
	}
	const editor = { tenantId: 'CLIENT_A', actor: 'ba@example.com', roles: ['CONFIG_EDITOR'] as any };

	it('creates a new configuration only from what the user chooses: nothing is copied by default', async () => {
		const api = homeApi();
		const go = vi.fn();
		render(Home, { api: api as any, me: editor, go });
		await vi.waitFor(() => expect(screen.getAllByRole('button', { name: 'Open' })).toHaveLength(2));
		await fireEvent.click(screen.getByRole('button', { name: '+ New configuration' }));
		const dialog = screen.getByRole('dialog');
		const start = within(dialog).getByLabelText('Start from') as HTMLSelectElement;
		expect(start.value).toBe(''); // nothing preselected
		const [code, title] = within(dialog).getAllByRole('textbox');
		await fireEvent.input(code, { target: { value: 'gl_motor_2027' } });
		await fireEvent.input(title, { target: { value: 'Motor 2027' } });
		await fireEvent.input(within(dialog).getByPlaceholderText('Why is this configuration needed?'), { target: { value: 'new motor book' } });
		const create = within(dialog).getByRole('button', { name: 'Create configuration' }) as HTMLButtonElement;
		expect(create.disabled).toBe(true); // the start is not chosen yet
		await fireEvent.change(start, { target: { value: 'GL_TEST_001@1' } });
		await vi.waitFor(() => expect(within(dialog).getByText('PREM_ADDED')).toBeTruthy());
		// Only PREM_ADDED is ticked.
		await fireEvent.click(within(dialog).getByText('PREM_ADDED').closest('label')!.querySelector('input')!);
		expect(within(dialog).getByText(/Will copy: 1 events, 1 postingRules, 1 sourceAuthority, 2 scenarios, 1 derivedMeasures, 1 datePolicies/)).toBeTruthy();
		await fireEvent.click(create);
		await vi.waitFor(() => expect(go).toHaveBeenCalledWith({ configId: 'GL_MOTOR_2027', version: 1 }));
		const [configId, t, , copied, reason] = (api.createConfig as any).mock.calls[0];
		expect([configId, t, reason]).toEqual(['GL_MOTOR_2027', 'Motor 2027', 'new motor book']);
		expect(copied.events.map((e: any) => e.code)).toEqual(['PREM_ADDED']);
		expect(copied.postingRules.map((r: any) => r.code)).toEqual(['PR_PREM_ADDED']);
		expect(copied.scenarios.map((r: any) => r.code)).toEqual(['E1', 'E1_HOME']);
		expect(copied.accounts.length).toBe(content.accounts.length); // reference data, as chosen
	});

	it('asks which version a new version starts from', async () => {
		const api = homeApi();
		const go = vi.fn();
		render(Home, { api: api as any, me: editor, go });
		await vi.waitFor(() => expect(screen.getByRole('button', { name: '+ New version' })).toBeTruthy());
		await fireEvent.click(screen.getByRole('button', { name: '+ New version' }));
		const dialog = screen.getByRole('dialog');
		const base = within(dialog).getByLabelText('Start from version') as HTMLSelectElement;
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'next year' } });
		expect((within(dialog).getByRole('button', { name: 'Create draft' }) as HTMLButtonElement).disabled).toBe(true);
		await fireEvent.change(base, { target: { value: '2' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Create draft' }));
		await vi.waitFor(() => expect(go).toHaveBeenCalledWith({ configId: 'GL_TEST_001', version: 3 }));
		expect((api.newDraft as any).mock.calls[0][1]).toBe(2);
	});

	it('shows a viewer why they cannot create', async () => {
		render(Home, { api: homeApi() as any, me: { ...editor, actor: 'viewer@example.com', roles: ['VIEWER'] as any }, go: vi.fn() });
		await vi.waitFor(() => expect(screen.getAllByRole('button', { name: 'Open' })).toHaveLength(2));
		expect((screen.getByRole('button', { name: '+ New configuration' }) as HTMLButtonElement).disabled).toBe(true);
		expect((screen.getByRole('button', { name: '+ New version' }) as HTMLButtonElement).disabled).toBe(true);
		expect(screen.getByText('Needs the CONFIG_EDITOR role.')).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'New draft' })).toBeNull();
	});
});

describe('configuring every section', () => {
	it('adds, replaces and removes items, storing only what was entered', () => {
		const acct = { code: '9948999', name: 'Fee receivable', class: 'AR0', activeFrom: '2026-01-01', entities: [], ledger: '', subledgerKey: '' };
		const added = withItem(content, specs.accounts, acct);
		expect(added.accounts.at(-1)).toEqual({ code: '9948999', name: 'Fee receivable', class: 'AR0', activeFrom: '2026-01-01' });
		const renamed = withItem(added, specs.accounts, { ...acct, name: 'Fee receivable (motor)' }, '9948999');
		expect(renamed.accounts.filter((a: any) => a.code === '9948999')).toHaveLength(1);
		expect(withItem(renamed, specs.accounts, null, '9948999').accounts.some((a: any) => a.code === '9948999')).toBe(false);
		expect(cleanItem(specs.dimensions.fields, { code: 'LOB', 'derivedBy': { lookup: '' }, values: ['MOTOR'], extra: 'kept' })).toEqual({ code: 'LOB', values: ['MOTOR'], extra: 'kept' });
	});
	it('creates a new event with its source authority', () => {
		const ev = { code: 'ENDORSEMENT', description: 'Endorsement posted', economicKey: ['POLICY_NO', 'TERM_NO'], measures: ['PREMIUM'], dimensions: ['COMPANY'], _sourceSystem: 'POLICY', _intakeMode: 'RAW_EVENT' };
		const c = withItem(content, specs.events, ev);
		expect(c.events.at(-1)).toEqual({ code: 'ENDORSEMENT', description: 'Endorsement posted', economicKey: ['POLICY_NO', 'TERM_NO'], measures: ['PREMIUM'], dimensions: ['COMPANY'] });
		expect(c.sourceAuthority.at(-1)).toEqual({ eventType: 'ENDORSEMENT', measure: '*', sourceSystem: 'POLICY', intakeMode: 'RAW_EVENT' });
	});
	it('adds an account number in a draft, checked by the server and saved with a reason', async () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 2 }, canEdit: true });
		ctx.version = { ...ctx.version, version: 2, status: 'draft' };
		render(SectionEditor, { ctx, spec: specs.accounts });
		await fireEvent.click(screen.getByRole('button', { name: '+ Add account' }));
		await fireEvent.input(screen.getByLabelText('Account number *'), { target: { value: '9948999' } });
		await fireEvent.input(screen.getByLabelText('Name *'), { target: { value: 'Fee receivable' } });
		await fireEvent.change(screen.getByLabelText('Accounting class *'), { target: { value: 'AR0' } });
		await fireEvent.input(screen.getByLabelText('Active from *'), { target: { value: '2026-01-01' } });
		await vi.waitFor(() => expect(ctx.api.check).toHaveBeenCalled());
		await fireEvent.click(screen.getByRole('button', { name: 'Save to draft v2' }));
		const dialog = screen.getByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'fee receivable account' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalledTimes(1));
		const [saved, reason] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('fee receivable account');
		expect(saved.accounts.at(-1)).toEqual({ code: '9948999', name: 'Fee receivable', class: 'AR0', activeFrom: '2026-01-01' });
	});
	it('in an approved version shows the items but offers a draft instead of editing', () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 1 } });
		render(SectionEditor, { ctx, spec: specs.accounts });
		expect(screen.queryByRole('button', { name: '+ Add account' })).toBeNull();
		expect(screen.getByText('9948523')).toBeTruthy();
		expect(screen.getByRole('button', { name: 'start a new draft from v1' })).toBeTruthy();
	});
});

describe('guided event flow', () => {
	it('lists the steps in the GL order, marks the stages not built, and moves forward', async () => {
		const ctx = ctxFor({ route: { configId: 'GL_TEST_001', version: 1, event: 'PREM_ADDED' } });
		(ctx.api as any).event = vi.fn(async () => prem);
		render(EventPage, { ctx });
		const nav = await screen.findByRole('navigation', { name: 'Steps for PREM_ADDED' });
		const labels = within(nav).getAllByRole('button').map((b) => b.querySelector('b')?.textContent);
		expect(labels).toEqual([
			'Client', 'Configuration version', 'Business event',
			'4. Event details', '5. Measures and formulas', '6. Dimensions, filters, dates, keys', '7. Accounting rules', '8. Debit/credit lines and accounts',
			'9. Required source fields', '10. Data Workbench mapping', '11. Data collection', '12. Rule execution (simulation)', '13. GL journal',
			'14. Subledger', '15. Reconciliation', '16. Reports and files'
		]);
		expect(within(nav).getAllByText('not built yet')).toHaveLength(5);
		await fireEvent.click(screen.getByRole('button', { name: '5. Measures and formulas →' }));
		expect(ctx.go).toHaveBeenCalledWith({ configId: 'GL_TEST_001', version: 1, event: 'PREM_ADDED', etab: 'measures' });
	});
});

describe('insurance event catalog', () => {
	const cat = {
		measures: [],
		dimensions: ['COMPANY', 'LOB', 'PRODUCT', 'BANK_ACCOUNT'].map((value) => ({ value, label: value })),
		events: eventCatalog as any
	};
	it('fills a new event from a template and declares the keys and dimensions it needs', () => {
		const item: any = { code: '', economicKey: [], measures: [], dimensions: [] };
		const tmpl = specs.events.fields.find((f) => f.key === '_template')!;
		tmpl.onset!(item, 'CLAIM_PAID', content, cat);
		expect(item).toMatchObject({ code: 'CLAIM_PAID', economicKey: ['CLAIM_NO', 'CLAIM_TXN_NO'], measures: ['CLAIM_PAYMENT', 'CLAIM_EXPENSE', 'EXCESS'] });
		item._sourceSystem = 'POLICY';
		const c = withItem(content, specs.events, item, undefined, cat);
		const keys = c.keyProfiles.map((k: any) => k.code);
		expect(keys).toEqual(expect.arrayContaining(['CLAIM_NO', 'CLAIM_TXN_NO', 'POLICY_NO']));
		expect(keys.filter((k: string) => k === 'POLICY_NO')).toHaveLength(1); // already there: not duplicated
		expect(c.dimensions.map((d: any) => d.code)).toEqual(expect.arrayContaining(['BANK_ACCOUNT', 'COMPANY', 'LOB']));
		expect(c.dimensions).toHaveLength(content.dimensions.length); // all three were already declared
		expect(c.sourceAuthority.at(-1)).toEqual({ eventType: 'CLAIM_PAID', measure: '*', sourceSystem: 'POLICY', intakeMode: 'RAW_EVENT' });
		expect(c.events.at(-1)).not.toHaveProperty('_template');
	});
	it('offers every template grouped by insurance area', () => {
		const opts = specs.events.fields.find((f) => f.key === '_template')!.options!(content, cat, {});
		expect(opts.length).toBe((eventCatalog as any).events.length);
		expect(opts.find((o) => o.value === 'NEW_BUSINESS')?.label).toBe('Policy lifecycle · NEW_BUSINESS — New business written');
	});
});
