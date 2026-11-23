// Stage 1 screens: they render a draft as it is, including an invalid one, and the analyst's
// changes are saved through the normal save with one reason.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { S1 } from './ctx';
import type { Content } from '../../lib/studio';
import Step3Assignments from './Step3Assignments.svelte';
import Step2Classes from './Step2Classes.svelte';

afterEach(cleanup);

const content: Content = {
	accountingClasses: [
		{ code: 'TAX_PAYABLE', name: 'Tax payable', nature: 'LIABILITY', standard: 'TAX_PAYABLE' },
		{ code: 'CASH', name: 'Cash at bank', nature: 'ASSET', custom: true }
	],
	accounts: [
		{ code: '230015', name: 'GST AU', activeFrom: '2026-07-01' },
		{ code: '230000', name: 'GST NZ', activeFrom: '2026-07-01' }
	],
	sourceClasses: [{ system: 'BILLING', code: 'PW0', meaning: 'Premium and tax source', mappings: [{ class: 'TAX_PAYABLE', when: { MEASURE_CODE: 'GST' } }] }],
	accountAssignmentRules: [
		{ code: 'AA_1', class: 'TAX_PAYABLE', account: '230015', match: { ENTITY: 'CML_AU' } },
		{ code: 'AA_2', class: 'TAX_PAYABLE', account: '230000', match: { ENTITY: 'CML_NZ' } },
		// a draft can hold mistakes: the same code twice must still show, so it can be fixed
		{ code: 'AA_2', class: 'CASH', account: '999999' }
	]
};

function ctxFor(over: Partial<S1> = {}): S1 {
	return {
		api: {
			tenant: 'CLIENT_A',
			assistantStatus: vi.fn(async () => ({ available: false, reason: 'External AI calls are switched off.' })),
			assign: vi.fn(),
			configs: vi.fn(async () => [])
		} as any,
		me: { tenantId: 'CLIENT_A', actor: 'ba@example.com', roles: ['CONFIG_EDITOR'] },
		version: { configId: 'X_COA', version: 1, status: 'draft', effectiveFrom: '2026-07-01', contentDigest: 'sha256:x', editors: ['ba@example.com'] } as any,
		content,
		canEdit: true,
		isEditor: true,
		isApprover: false,
		library: {
			catalog: 'gl.classes.standard/v1',
			version: '0.1',
			status: 'PROPOSED',
			note: '',
			classes: [{ code: 'TAX_PAYABLE', group: 'Payables', name: 'Tax payable', definition: 'Indirect tax owed.', nature: 'LIABILITY', normalSide: 'C', status: 'PROPOSED' }]
		},
		systems: ['BILLING'],
		files: [],
		addFiles: vi.fn(),
		save: vi.fn(async () => {}),
		reload: vi.fn(async () => {}),
		step: vi.fn(),
		...over
	};
}

describe('Stage 1 screens', () => {
	it('step 3 shows rules with conditions, opens one, and flags an account not in the chart', async () => {
		render(Step3Assignments, { ctx: ctxFor() });
		const rules = screen.getByLabelText('Assignment rules');
		expect(within(rules).getAllByText('ENTITY = CML_AU').length).toBe(1);
		expect(within(rules).getByText('not in the chart of accounts')).toBeTruthy();
		await fireEvent.click(within(rules).getAllByText('AA_1')[0]);
		const form = await screen.findByLabelText('Rule');
		expect((within(form).getAllByLabelText('Condition')[0] as HTMLInputElement).value).toBe('ENTITY');
		// the condition suggestions offer ENTITY once, though rules and the defaults both name it
		const opts = [...document.querySelectorAll('#ws-ckeys option')].map((o) => (o as HTMLOptionElement).value);
		expect(opts.filter((o) => o === 'ENTITY').length).toBe(1);
	});

	it('step 3 adds a rule by hand and saves the changes with one reason', async () => {
		const ctx = ctxFor();
		render(Step3Assignments, { ctx });
		await fireEvent.click(screen.getByRole('button', { name: '+ Add rule' }));
		const form = await screen.findByLabelText('Rule');
		await fireEvent.change(within(form).getByLabelText(/^Class/), { target: { value: 'CASH' } });
		await fireEvent.input(within(form).getByLabelText(/^Account/), { target: { value: '230000' } });
		await fireEvent.click(within(form).getByRole('button', { name: 'Add to my changes' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Save changes…' }));
		const dialog = screen.getByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'cash account' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalledTimes(1));
		const [saved, reason] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('cash account');
		expect(saved.accountAssignmentRules.at(-1)).toMatchObject({ class: 'CASH', account: '230000' });
		expect(saved.accountAssignmentRules).toHaveLength(4);
	});

	it('step 2 keeps the source code as received and says the library is not approved yet', async () => {
		render(Step2Classes, { ctx: ctxFor() });
		expect(screen.getByText(/standard class library v0.1 is proposed/)).toBeTruthy();
		const src = screen.getByLabelText('Source classes');
		expect(within(src).getByText('PW0')).toBeTruthy();
		expect(within(src).getByText(/when MEASURE_CODE=GST/)).toBeTruthy();
		await fireEvent.click(within(src).getByText('PW0'));
		const panel = await screen.findByLabelText('Mapping');
		expect(within(panel).getByText('library class not approved yet')).toBeTruthy();
	});
});
