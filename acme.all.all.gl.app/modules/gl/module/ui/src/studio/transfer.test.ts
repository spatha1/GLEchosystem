// ALTB-11 — the Import and export page: download a version, import a file into a draft with a
// preview, a server check and one reason.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Ctx } from '../lib/ctx';
import type { Content } from '../lib/studio';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';
import { fromHash, toHash } from '../lib/route';
import { versionViews } from '../lib/views';
import ImportExportPage from './ImportExportPage.svelte';

afterEach(cleanup);
const content = structuredClone(fixtureConfig) as unknown as Content;

function ctxFor(over: Partial<Ctx> = {}): Ctx {
	return {
		api: { check: vi.fn(async () => ({ ok: true, findings: [], contentDigest: 'sha256:x', changes: [] })) } as any,
		me: { tenantId: 'CLIENT_A', actor: 'ba@example.com', roles: ['CONFIG_EDITOR'] },
		version: {
			tenantId: 'CLIENT_A',
			configId: 'GL_TEST_001',
			version: 2,
			status: 'draft',
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
		canEdit: true,
		isEditor: true,
		route: { configId: 'GL_TEST_001', version: 2, tab: 'transfer' },
		go: vi.fn(),
		save: vi.fn(async () => {}),
		startDraft: vi.fn(),
		tick: 0,
		catalog: { measures: [], dimensions: [] },
		...over
	} as Ctx;
}

const csv = (name: string, text: string) => new File([text], name, { type: 'text/csv' });

describe('import and export', () => {
	it('is a view of every version, linked by its URL', () => {
		expect(versionViews.find((v) => v.key === 'transfer')?.label).toBe('Import and export');
		expect(fromHash(toHash({ configId: 'GL_TEST_001', version: 2, tab: 'transfer' }))).toEqual({ configId: 'GL_TEST_001', version: 2, tab: 'transfer' });
	});

	it('previews a CSV import, has it checked by the server, and saves it with one reason', async () => {
		const ctx = ctxFor();
		render(ImportExportPage, { ctx });
		const first = content.accounts[0].code;
		await fireEvent.change(screen.getByLabelText('Files to import'), {
			target: { files: [csv('accounts.csv', `code,name\r\n${first},Renamed\r\nNEW1,A new account\r\n`)] }
		});
		const preview = await screen.findByLabelText('Import preview');
		await vi.waitFor(() => expect(ctx.api.check).toHaveBeenCalled());
		const row = within(preview).getByRole('row', { name: /Accounts/ });
		expect(within(row).getAllByRole('cell').slice(1, 5).map((c) => c.textContent)).toEqual(['1', '1', '0', String(content.accounts.length - 1)]);
		await screen.findByText(/The draft checks out after the import/);
		await fireEvent.click(screen.getByRole('button', { name: 'Save 2 changes to draft' }));
		const dialog = screen.getByRole('dialog');
		expect(dialog.textContent).toMatch(/1 added, 1 changed, 0 removed, in 1 section, from accounts.csv/);
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'chart of accounts from the client' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save to draft' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalledTimes(1));
		const [saved, reason] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('chart of accounts from the client');
		expect(saved.accounts[0].name).toBe('Renamed');
		expect(saved.accounts.at(-1)).toEqual({ code: 'NEW1', name: 'A new account' });
		await screen.findByText(/Saved to draft v2: 1 added, 1 changed, 0 removed, in one change/);
	});

	it('blocks saving while the file has problems, and says which', async () => {
		const ctx = ctxFor();
		render(ImportExportPage, { ctx });
		await fireEvent.change(screen.getByLabelText('Files to import'), { target: { files: [csv('postingRules.csv', 'code,rank\r\nR1,ten\r\n')] } });
		const alert = await screen.findByText(/1 problem in the file/);
		expect(alert.closest('[role=alert]')?.textContent).toMatch(/postingRules row 2, rank: "ten" is not a number/);
		expect(screen.queryByRole('button', { name: /Save .* to draft/ })).toBeNull();
		expect(ctx.api.check).not.toHaveBeenCalled();
	});

	it('in an approved version offers the downloads and a draft, not an import', () => {
		const ctx = ctxFor({ canEdit: false, version: { ...ctxFor().version, version: 1, status: 'approved' } });
		render(ImportExportPage, { ctx });
		expect(screen.getByRole('button', { name: 'Download workbook (.xlsx)' })).toBeTruthy();
		expect(screen.queryByLabelText('Files to import')).toBeNull();
		screen.getByRole('button', { name: 'New draft from v1' }).click();
		expect(ctx.startDraft).toHaveBeenCalledWith({ tab: 'transfer' });
	});
});
