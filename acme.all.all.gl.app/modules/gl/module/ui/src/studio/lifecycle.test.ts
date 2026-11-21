// ALTB-04: the lifecycle preview panel. The responses are recorded from the real API over the
// GL-TEST-001 configuration (src/test/*.lifecycle.json): E1 → E2 → E3, and one that stops.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Ctx } from '../lib/ctx';
import type { Content, LifecycleResult } from '../lib/studio';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';
import lifeRaw from '../test/e1-e3.lifecycle.json';
import stopRaw from '../test/stopped.lifecycle.json';
import LifecyclePanel from './LifecyclePanel.svelte';
import FieldsTable from './FieldsTable.svelte';
import { pickEvents, withProfile } from '../lib/content';
import type { EventExplanation, ManifestEvent } from '../lib/studio';

const life = (lifeRaw as unknown as { lifecycle: LifecycleResult }).lifecycle;
const stopped = (stopRaw as unknown as { lifecycle: LifecycleResult }).lifecycle;

afterEach(cleanup);

function ctxWith(result: LifecycleResult): Ctx {
	return {
		api: { lifecycle: vi.fn(async () => result) } as any,
		me: { tenantId: 'CLIENT_A', actor: 'viewer@example.com', roles: ['VIEWER'] },
		version: { configId: 'GL_TEST_001', version: 1, status: 'approved' } as any,
		content: structuredClone(fixtureConfig) as unknown as Content,
		canEdit: false,
		isEditor: false,
		route: { configId: 'GL_TEST_001', version: 1, tab: 'simulate' },
		go: vi.fn(),
		save: vi.fn(),
		startDraft: vi.fn(),
		tick: 0,
		catalog: { measures: [], dimensions: [] }
	};
}

async function addStep(code: string) {
	await fireEvent.change(screen.getByRole('combobox', { name: /Add a scenario/ }), { target: { value: code } });
	await fireEvent.click(screen.getByRole('button', { name: 'Add step' }));
}

describe('lifecycle preview', () => {
	it('starts empty: nothing is chosen until the user adds a step, and it says nothing is posted', () => {
		const ctx = ctxWith(life);
		render(LifecyclePanel, { ctx });
		expect(screen.getByText('Preview · not posted')).toBeTruthy();
		expect(screen.getByText(/No steps yet/)).toBeTruthy();
		expect((screen.getByRole('button', { name: 'Run lifecycle' }) as HTMLButtonElement).disabled).toBe(true);
		expect((screen.getByRole('combobox', { name: /Add a scenario/ }) as HTMLSelectElement).value).toBe('');
	});

	it('runs the steps in the order chosen and shows the temporary ledger after each', async () => {
		const ctx = ctxWith(life);
		render(LifecyclePanel, { ctx });
		await addStep('E2');
		await addStep('E1');
		await addStep('E3');
		await fireEvent.click(screen.getByRole('button', { name: 'Move E1 up' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Run lifecycle' }));
		await vi.waitFor(() => expect(screen.getByText('Every step can post')).toBeTruthy());
		expect((ctx.api.lifecycle as any).mock.calls[0][2]).toEqual([{ scenario: 'E1' }, { scenario: 'E2' }, { scenario: 'E3' }]);
		expect(screen.getByText(/3 steps · 12 lines · 4 journal entries/)).toBeTruthy();
		expect(screen.getByText('Not posted')).toBeTruthy();

		// The last step is shown first: after E3, INV2 has 0.50 left and the intercompany pair is open.
		const ledger = screen.getByRole('heading', { name: 'Temporary ledger after step 3' }).closest('section')!;
		const inv2 = within(ledger).getByText('INVOICE_NO INV2').closest('tr')!;
		expect(within(inv2).getByText('0.50')).toBeTruthy();
		expect(within(ledger).getAllByText('1,099.50').length).toBeGreaterThan(0);

		// Step 1 alone: only E1's policy receivable, premium and tax.
		await fireEvent.click(screen.getAllByRole('button', { name: 'Show' })[0]);
		const after1 = screen.getByRole('heading', { name: 'Temporary ledger after step 1' }).closest('section')!;
		expect(within(after1).getAllByRole('row')).toHaveLength(1 + 3);
		const journal1 = screen.getByRole('heading', { name: /Step 1 journal/ }).closest('section')!;
		expect(within(journal1).getAllByText('▲ increases')).toHaveLength(3);
	});

	it('stops at a step that cannot post, says why, and does not run the rest', async () => {
		const ctx = ctxWith(stopped);
		render(LifecyclePanel, { ctx });
		await addStep('E1');
		await addStep('E3_SAME_COMPANY');
		await addStep('E2');
		await fireEvent.click(screen.getByRole('button', { name: 'Run lifecycle' }));
		await vi.waitFor(() => expect(screen.getByText('Stopped')).toBeTruthy());
		expect(screen.getByRole('alert').textContent).toMatch(/step 2 \(E3_SAME_COMPANY\) cannot be posted: ENTITY_NOT_ALLOWED/);
		expect(screen.getAllByRole('button', { name: /^Show/ })).toHaveLength(2); // E2 was not run
		expect(screen.getByRole('heading', { name: /Step 2 journal/ })).toBeTruthy(); // the failing step is shown
	});

	it('removes a step and clears an earlier result', async () => {
		const ctx = ctxWith(life);
		render(LifecyclePanel, { ctx });
		await addStep('E1');
		await fireEvent.click(screen.getByRole('button', { name: 'Run lifecycle' }));
		await vi.waitFor(() => expect(screen.getByText('Every step can post')).toBeTruthy());
		await fireEvent.click(screen.getByRole('button', { name: 'Remove E1' }));
		expect(screen.queryByText('Every step can post')).toBeNull();
		expect(screen.getByText(/No steps yet/)).toBeTruthy();
	});
});

describe('ledger reads in the field manifest', () => {
	it('lists a ledger read as calculated by GL, read from the ledger, and never as a field to collect', () => {
		const why = [{ kind: 'FORMULA', ref: 'WO_AMT', role: 'INPUT', detail: 'input of WO_AMT', via: [], mandatory: true }];
		const explanation = {
			eventType: 'INVOICE_WRITTEN_OFF',
			sourceSystems: ['BILLING'],
			fields: [
				{ fieldCode: 'INVOICE_NO', kind: 'SOURCE', category: 'KEY', label: 'Invoice number', node: 'field:INVOICE_NO', mandatory: true, reasons: why },
				{ fieldCode: 'OPEN_BALANCE_AR1_INVOICE_NO', kind: 'LEDGER_STATE', category: 'LEDGER_STATE', label: "Open balance of AR1 for the event's INVOICE_NO",
					detail: "OPEN_BALANCE('AR1', 'INVOICE_NO'): read from posted lines, never requested from a source", node: 'ledger:OPEN_BALANCE_AR1_INVOICE_NO',
					mandatory: true, derivedFrom: ['INVOICE_NO'], reasons: why }
			],
			lists: [],
			unused: []
		} as unknown as EventExplanation;
		const manifest = { eventType: 'INVOICE_WRITTEN_OFF', fields: [
			{ fieldCode: 'INVOICE_NO', kind: 'SOURCE', dataType: 'STRING', cardinality: 'ONE', mandatory: true, requiredBy: [{ kind: 'FORMULA', ref: 'WO_AMT' }] },
			{ fieldCode: 'OPEN_BALANCE_AR1_INVOICE_NO', kind: 'LEDGER_STATE', dataType: 'DECIMAL', cardinality: 'ONE', mandatory: true, derivedFrom: ['INVOICE_NO'], requiredBy: [{ kind: 'FORMULA', ref: 'WO_AMT' }] }
		] } as unknown as ManifestEvent;
		render(FieldsTable, { explanation, manifest });
		const calc = screen.getByRole('heading', { name: /Calculated by GL/ }).closest('section')!;
		expect(within(calc).getByText('OPEN_BALANCE_AR1_INVOICE_NO')).toBeTruthy();
		expect(within(calc).getByText('read from the ledger')).toBeTruthy();
		const collect = screen.getByRole('heading', { name: /Fields to collect/ }).closest('section')!;
		expect(within(collect).queryByText('OPEN_BALANCE_AR1_INVOICE_NO')).toBeNull();
		expect(within(collect).getByText('INVOICE_NO')).toBeTruthy();
	});
});

describe('lifecycle profiles (ALTB-07)', () => {
	const withProfiles = (canEdit: boolean) => {
		const ctx = ctxWith(life);
		(ctx.content as any).lifecycleProfiles = [{ code: 'P1_JANUARY', description: 'Policy P1: premium, invoice, payment', steps: ['E1', 'E2', 'E3'] }];
		ctx.canEdit = canEdit;
		ctx.save = vi.fn(async () => {});
		return ctx;
	};

	it('starts from a profile only when the user picks one, then runs its steps', async () => {
		const ctx = withProfiles(false);
		render(LifecyclePanel, { ctx });
		const pick = screen.getByRole('combobox', { name: /Start from a profile/ }) as HTMLSelectElement;
		expect(pick.value).toBe('');
		expect(screen.getByText(/No steps yet/)).toBeTruthy();
		await fireEvent.change(pick, { target: { value: 'P1_JANUARY' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Run lifecycle' }));
		await vi.waitFor(() => expect((ctx.api.lifecycle as any).mock.calls[0][2]).toEqual([{ scenario: 'E1' }, { scenario: 'E2' }, { scenario: 'E3' }]));
		expect(screen.queryByRole('group', { name: 'Save as profile' })).toBeNull(); // not editable: no saving
	});

	it('saves the steps as a profile in the draft, with a reason', async () => {
		const ctx = withProfiles(true);
		render(LifecyclePanel, { ctx });
		await addStep('E1');
		await addStep('E2');
		const group = screen.getByRole('group', { name: 'Save as profile' });
		const save = within(group).getByRole('button', { name: 'Save as profile in draft' }) as HTMLButtonElement;
		expect(save.disabled).toBe(true); // a code is needed
		await fireEvent.input(within(group).getByLabelText(/Profile code/), { target: { value: 'p1_invoiced' } });
		await fireEvent.click(save);
		const dialog = await screen.findByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'Premium then invoice' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalled());
		const [content, reason] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('Premium then invoice');
		expect(content.lifecycleProfiles).toEqual([
			{ code: 'P1_JANUARY', description: 'Policy P1: premium, invoice, payment', steps: ['E1', 'E2', 'E3'] },
			{ code: 'P1_INVOICED', steps: ['E1', 'E2'] }
		]);
	});
});

describe('profiles in a copied configuration (ALTB-07)', () => {
	it('keeps a profile only when every scenario it names is copied', () => {
		let c = structuredClone(fixtureConfig) as unknown as Content;
		c = withProfile(c, { code: 'P1_JANUARY', steps: ['E1', 'E2', 'E3'] });
		c = withProfile(c, { code: 'P1_PREMIUM', steps: ['E1'] });
		const premiumOnly = pickEvents(c, ['PREM_ADDED'], false);
		expect((premiumOnly.lifecycleProfiles as any[]).map((p) => p.code)).toEqual(['P1_PREMIUM']);
		const all = pickEvents(c, ['PREM_ADDED', 'INVOICED', 'PAYMENT_RECEIVED'], false);
		expect((all.lifecycleProfiles as any[]).map((p) => p.code)).toEqual(['P1_JANUARY', 'P1_PREMIUM']);
	});
});
