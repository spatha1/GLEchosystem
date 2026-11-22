// ALTB-06: the Ledger area (sandbox books). The responses are recorded from the real API: book
// SANDBOX_JAN posting E1 → E2 → E3 of GL-TEST-001 (src/test/sandbox-*.json).
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../lib/types';
import type { SandboxBook, SandboxBookView, SandboxPostResult } from '../lib/studio';
import { fromHash, toHash } from '../lib/route';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';
import bookRaw from '../test/sandbox-book.json';
import booksRaw from '../test/sandbox-books.json';
import postRaw from '../test/sandbox-post.json';
import LedgerSandbox from './LedgerSandbox.svelte';

const bookView = bookRaw as unknown as SandboxBookView;
const books = (booksRaw as unknown as { books: SandboxBook[] }).books;
const posted = postRaw as unknown as SandboxPostResult;

afterEach(cleanup);

const viewer: Me = { tenantId: 'CLIENT_B', actor: 'viewer@example.com', roles: ['VIEWER'] };
const operator: Me = { tenantId: 'CLIENT_B', actor: 'ops@example.com', roles: ['OPERATOR'] };

function apiWith(over: Record<string, unknown> = {}) {
	return {
		sandboxBooks: vi.fn(async () => books),
		sandboxBook: vi.fn(async () => bookView),
		version: vi.fn(async () => ({ content: structuredClone(fixtureConfig) })),
		configs: vi.fn(async () => [{ configId: 'GL_TEST_001', title: '', createdBy: '', createdAt: '', latestVersion: 1 }]),
		versions: vi.fn(async () => [{ version: 1, status: 'approved' }]),
		openSandboxBook: vi.fn(async () => books[0]),
		postToSandbox: vi.fn(async () => posted),
		...over
	} as any;
}

describe('ledger routes', () => {
	it('live apart from the studio and round-trip through the URL', () => {
		expect(toHash({ ledger: true })).toBe('#/ledger');
		expect(toHash({ ledger: true, book: 'SANDBOX_JAN' })).toBe('#/ledger/b/SANDBOX_JAN');
		expect(fromHash('#/ledger/b/SANDBOX_JAN')).toEqual({ ledger: true, book: 'SANDBOX_JAN' });
		expect(fromHash('#/ledger')).toEqual({ ledger: true });
		expect(fromHash('#/c/GL_TEST_001/v/1').ledger).toBeUndefined();
	});
});

describe('sandbox books', () => {
	it('says every book is a sandbox, lists them, and lets only an operator open one', async () => {
		const go = vi.fn();
		render(LedgerSandbox, { api: apiWith(), me: viewer, go });
		expect(screen.getByRole('note').textContent).toMatch(/synthetic data only/);
		const link = await screen.findByRole('button', { name: 'SANDBOX_JAN' });
		expect(screen.getByText('GL_TEST_001 v1')).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Open a sandbox book' })).toBeNull();
		expect(screen.getByText(/Only an operator opens books/)).toBeTruthy();
		await fireEvent.click(link);
		expect(go).toHaveBeenCalledWith({ ledger: true, book: 'SANDBOX_JAN' });
	});

	it('opens a book on an approved version, with a reason', async () => {
		const api = apiWith();
		const go = vi.fn();
		render(LedgerSandbox, { api, me: operator, go });
		await fireEvent.click(await screen.findByRole('button', { name: 'Open a sandbox book' }));
		const form = await screen.findByRole('form', { name: 'Open a sandbox book' });
		await vi.waitFor(() => expect(within(form).getByRole('option', { name: 'GL_TEST_001 v1' })).toBeTruthy());
		const open = within(form).getByRole('button', { name: 'Open book' }) as HTMLButtonElement;
		expect(open.disabled).toBe(true); // nothing chosen yet
		await fireEvent.input(within(form).getByLabelText(/Book id/), { target: { value: 'sandbox_feb' } });
		await fireEvent.change(within(form).getByLabelText(/Posts with/), { target: { value: 'GL_TEST_001@1' } });
		await fireEvent.input(within(form).getByLabelText(/Why/), { target: { value: 'February run' } });
		await fireEvent.click(open);
		await vi.waitFor(() => expect(api.openSandboxBook).toHaveBeenCalledWith('SANDBOX_FEB', 'GL_TEST_001', 1, 'February run'));
	});
});

describe('one sandbox book', () => {
	it('shows its postings, their C6 journals and a trial balance that balances per entity', async () => {
		render(LedgerSandbox, { api: apiWith(), me: viewer, book: 'SANDBOX_JAN', go: vi.fn() });
		await screen.findByRole('heading', { name: /Postings/ });
		expect(screen.queryByRole('region', { name: 'Post synthetic events' })).toBeNull(); // a viewer only reads
		const postings = screen.getByRole('heading', { name: /Postings/ }).closest('section')!;
		expect(within(postings).getAllByRole('button', { name: 'Journals' })).toHaveLength(3);
		await fireEvent.click(within(postings).getAllByRole('button', { name: 'Journals' })[2]); // E3: two entries
		expect(within(postings).getAllByText(/^J:PAYMENT_RECEIVED:R1:C0[19]:AUD$/)).toHaveLength(2);
		expect(within(postings).getAllByText('INVOICE_NO:INV2').length).toBeGreaterThan(0);

		const tb = screen.getByRole('region', { name: 'Trial balance' });
		const inv2 = within(tb).getByText('INVOICE_NO INV2').closest('tr')!;
		expect(within(inv2).getByText('0.50')).toBeTruthy();
		const checks = within(tb).getAllByText(/debit balances .* = credit balances/);
		expect(checks).toHaveLength(2); // C01 and C09
		for (const c of checks) expect(c.className).toContain('ok');
	});

	it('lets an operator post scenarios in order; nothing is chosen until they add one', async () => {
		const api = apiWith();
		render(LedgerSandbox, { api, me: operator, book: 'SANDBOX_JAN', go: vi.fn() });
		const card = await screen.findByRole('region', { name: 'Post synthetic events' });
		const pick = await within(card).findByLabelText(/Add a scenario/);
		expect((pick as HTMLSelectElement).value).toBe('');
		expect((within(card).getByRole('button', { name: 'Post to sandbox' }) as HTMLButtonElement).disabled).toBe(true);
		for (const code of ['E2', 'E1']) {
			await vi.waitFor(() => expect(within(card).getByRole('option', { name: new RegExp(`^${code} ·`) })).toBeTruthy());
			await fireEvent.change(pick, { target: { value: code } });
			await fireEvent.click(within(card).getByRole('button', { name: 'Add' }));
		}
		await fireEvent.click(within(card).getByRole('button', { name: 'Move E1 up' }));
		await fireEvent.click(within(card).getByRole('button', { name: 'Post to sandbox' }));
		await vi.waitFor(() => expect(api.postToSandbox).toHaveBeenCalledWith('SANDBOX_JAN', [{ scenario: 'E1' }, { scenario: 'E2' }]));
		expect((await within(card).findByRole('status')).textContent).toMatch(/Posted 3 events/);
	});

	it('shows why nothing was posted when the engine refuses an event', async () => {
		const api = apiWith({
			postToSandbox: vi.fn(async () => {
				throw { status: 422, code: 'POSTING_REFUSED', message: 'nothing was posted: step 1 (E3_SAME_COMPANY) cannot be posted: ENTITY_NOT_ALLOWED' };
			})
		});
		render(LedgerSandbox, { api, me: operator, book: 'SANDBOX_JAN', go: vi.fn() });
		const card = await screen.findByRole('region', { name: 'Post synthetic events' });
		const pick = within(card).getByLabelText(/Add a scenario/);
		await vi.waitFor(() => expect(within(card).getByRole('option', { name: /^E3_SAME_COMPANY ·/ })).toBeTruthy());
		await fireEvent.change(pick, { target: { value: 'E3_SAME_COMPANY' } });
		await fireEvent.click(within(card).getByRole('button', { name: 'Add' }));
		await fireEvent.click(within(card).getByRole('button', { name: 'Post to sandbox' }));
		expect((await within(card).findByRole('alert')).textContent).toMatch(/POSTING_REFUSED: nothing was posted.*ENTITY_NOT_ALLOWED/);
	});

	it('posts a profile of the pinned version when the operator picks it', async () => {
		const content = structuredClone(fixtureConfig) as any;
		content.lifecycleProfiles = [{ code: 'P1_JANUARY', steps: ['E1', 'E2', 'E3'] }];
		const api = apiWith({ version: vi.fn(async () => ({ content })) });
		render(LedgerSandbox, { api, me: operator, book: 'SANDBOX_JAN', go: vi.fn() });
		const card = await screen.findByRole('region', { name: 'Post synthetic events' });
		const pick = await within(card).findByLabelText(/Start from a profile/);
		await fireEvent.change(pick, { target: { value: 'P1_JANUARY' } });
		await fireEvent.click(within(card).getByRole('button', { name: 'Post to sandbox' }));
		await vi.waitFor(() => expect(api.postToSandbox).toHaveBeenCalledWith('SANDBOX_JAN', [{ scenario: 'E1' }, { scenario: 'E2' }, { scenario: 'E3' }]));
	});
});

describe('reversing journals in a book (ALTB-09)', () => {
	it('shows the trial balance as at a date and labels reversal entries', async () => {
		const withRev = structuredClone(bookView) as any;
		withRev.asOf = '2026-07-15';
		const j = withRev.postings[0].journals[0];
		withRev.postings[0].journals.push({ ...structuredClone(j), journalId: j.journalId + ':REV', reversalOf: j.journalId, bookingDate: '2026-08-01', journalType: 'RJ' });
		const api = apiWith({ sandboxBook: vi.fn(async () => withRev) });
		render(LedgerSandbox, { api, me: viewer, book: 'SANDBOX_JAN', go: vi.fn() });
		const tb = await screen.findByRole('region', { name: 'Trial balance' });
		const asOf = within(tb).getByLabelText(/As at/) as HTMLInputElement;
		expect(asOf.value).toBe('2026-07-15');
		await fireEvent.change(asOf, { target: { value: '2026-08-01' } });
		await vi.waitFor(() => expect(api.sandboxBook).toHaveBeenLastCalledWith('SANDBOX_JAN', '2026-08-01'));
		const postings = screen.getByRole('heading', { name: /Postings/ }).closest('section')!;
		await fireEvent.click(within(postings).getAllByRole('button', { name: 'Journals' })[0]);
		expect(within(postings).getByText(`reversal of ${j.journalId}, 2026-08-01`)).toBeTruthy();
	});
});
