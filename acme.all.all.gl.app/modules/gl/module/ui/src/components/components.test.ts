import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Me, PlannedLine, PostingPlan, Version } from '../lib/types';
import PlanView from './PlanView.svelte';
import VersionActions from './VersionActions.svelte';

afterEach(cleanup);

const ref = { configId: 'DEMO_GL', configVersion: 1, contentDigest: 'sha256:00' };
const leg = (lineNo: number, cls: string, side: 'D' | 'C', amount: string, account: string): PlannedLine => ({
	lineNo,
	template: cls,
	class: cls,
	side,
	amount,
	currency: 'AUD',
	entity: 'C01',
	keys: { POLICY_NO: 'P1' },
	dimensions: { LOB: 'MOTOR' },
	explanation: `${cls} ${side}`,
	trace: { reads: [{ name: 'GROSS', value: '1100 AUD' }] },
	account,
	ledger: 'PRIMARY',
	assignment: { rule: `AA_${cls}`, account, ledger: 'PRIMARY', config: ref, why: ['LOB = MOTOR (exact)'], considered: [{ rule: `AA_${cls}`, account, priority: 0, matched: true, reason: 'LOB = MOTOR (exact)' }] }
});

// E1 of GL-TEST-001 as the API returns it.
const e1: PostingPlan = {
	eventType: 'PREM_ADDED',
	rule: 'PR_PREM_ADDED',
	rank: 10,
	config: ref,
	considered: [{ rule: 'PR_PREM_ADDED', rank: 10, matched: true, reason: 'no filter: applies to every PREM_ADDED' }],
	lines: [leg(1, 'AR0', 'D', '1100.00', '9948523'), leg(2, 'PW0', 'C', '1000.00', '4100100'), leg(3, 'TX0', 'C', '100.00', '2200100')],
	skipped: [],
	totals: [{ entity: 'C01', currency: 'AUD', debit: '1100.00', credit: '1100.00', balanced: true }],
	balanced: true,
	bookDate: '2026-01-01'
};

describe('posting preview', () => {
	it('shows each leg in its debit or credit column with its account', () => {
		const { container } = render(PlanView, { plan: e1 });
		const rows = container.querySelectorAll('tbody tr');
		const first = rows[0] as HTMLElement;
		expect(first.querySelector('[data-side="D"]')!.textContent).toBe('1,100.00 AUD');
		expect(first.querySelector('[data-side="C"]')!.textContent).toBe('');
		expect(within(first).getByText('9948523')).toBeTruthy();
		const second = rows[1] as HTMLElement;
		expect(second.querySelector('[data-side="C"]')!.textContent).toBe('1,000.00 AUD');
		expect(screen.getAllByText('Balanced').length).toBeGreaterThan(0);
		expect(screen.getByText('✓ balanced')).toBeTruthy();
		expect(screen.getByText('Can post')).toBeTruthy();
	});
	it('explains a leg: amount lineage and the account rule with its version', async () => {
		render(PlanView, { plan: e1 });
		await fireEvent.click(screen.getByRole('button', { name: 'Why line 1' }));
		expect(screen.getByText('Read: GROSS = 1100 AUD')).toBeTruthy();
		expect(screen.getByText('AA_AR0', { selector: 'p code' })).toBeTruthy();
		expect(screen.getByText(/configuration DEMO_GL v1/)).toBeTruthy();
	});
	it('marks an unbalanced plan and a refused leg', () => {
		const bad: PostingPlan = {
			...e1,
			balanced: false,
			totals: [{ entity: 'C01', currency: 'AUD', debit: '1100.00', credit: '1000.00', balanced: false }],
			lines: [{ ...e1.lines[0], problem: 'MISSING_ACCOUNT: no assignment rule for class AR0' }, e1.lines[1]]
		};
		render(PlanView, { plan: bad });
		expect(screen.getByText('Not balanced')).toBeTruthy();
		expect(screen.getByText('Refused')).toBeTruthy();
		expect(screen.getByText('1 leg without a usable account')).toBeTruthy();
		expect(screen.getByRole('alert').textContent).toMatch(/MISSING_ACCOUNT/);
	});
});

describe('version actions', () => {
	const v: Version = {
		tenantId: 'DEMO',
		configId: 'DEMO_GL',
		version: 2,
		status: 'validated',
		effectiveFrom: '2026-02-01',
		effectiveTo: null,
		contentDigest: 'sha256:00',
		createdBy: 'lead@example.com',
		createdAt: '',
		updatedBy: 'lead@example.com',
		updatedAt: '',
		editors: ['lead@example.com']
	};
	it('disables approve for the person who changed the version, and says why', async () => {
		const lead: Me = { tenantId: 'DEMO', actor: 'lead@example.com', roles: ['CONFIG_EDITOR', 'CONFIG_APPROVER'] };
		const onaction = vi.fn();
		render(VersionActions, { version: v, me: lead, onaction, onpreview: vi.fn() });
		const approve = screen.getByRole('button', { name: 'Approve' }) as HTMLButtonElement;
		expect(approve.disabled).toBe(true);
		expect(screen.getByText(/another approver must approve it/)).toBeTruthy();
		await fireEvent.click(approve);
		expect(onaction).not.toHaveBeenCalled();
	});
	it('lets another approver approve', async () => {
		const reviewer: Me = { tenantId: 'DEMO', actor: 'reviewer@example.com', roles: ['CONFIG_APPROVER'] };
		const onaction = vi.fn();
		render(VersionActions, { version: v, me: reviewer, onaction, onpreview: vi.fn() });
		await fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
		expect(onaction).toHaveBeenCalledWith('approve', v);
	});
});
