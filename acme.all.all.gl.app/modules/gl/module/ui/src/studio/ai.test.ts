// Configure from a BRD: the helpers that turn a chosen proposal into the draft's content, and
// the screen's flow (upload, ask, choose, approve) on the GL-TEST-001 configuration.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Ctx } from '../lib/ctx';
import type { Content } from '../lib/studio';
import { applyItems, chosenItems, gapItems, type AIProposal } from '../lib/ai';
import AIWizard from './AIWizard.svelte';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';

const content = structuredClone(fixtureConfig) as unknown as Content;
afterEach(() => {
	cleanup();
	try {
		sessionStorage.clear();
	} catch {
		/* none */
	}
});

const proposal = (over: Partial<AIProposal> = {}): AIProposal => ({
	step: 'events',
	provider: 'openai',
	model: 'test-double',
	brd: 'motor-renewals-brd.md',
	items: [
		{ id: 'keyProfiles/INTERMEDIARY_ID', section: 'keyProfiles', key: 'INTERMEDIARY_ID', group: '', change: 'add', label: 'Business key INTERMEDIARY_ID', evidence: [], value: { code: 'INTERMEDIARY_ID' } },
		{
			id: 'events/RENEWAL',
			section: 'events',
			key: 'RENEWAL',
			group: 'RENEWAL',
			change: 'add',
			label: 'RENEWAL · Policy renewed',
			why: 'From the insurance catalog template RENEWAL.',
			confidence: 'high',
			evidence: [
				{ quote: 'the policy system sends a renewal event', found: true },
				{ quote: 'Renewals are booked every Friday', found: false }
			],
			value: { code: 'RENEWAL', economicKey: ['POLICY_NO', 'TERM_NO'], keys: ['POLICY_NO', 'TERM_NO', 'INTERMEDIARY_ID'], dimensions: ['COMPANY', 'LOB'] }
		},
		{
			id: 'sourceAuthority/RENEWAL',
			section: 'sourceAuthority',
			key: 'RENEWAL|*|',
			group: 'RENEWAL',
			change: 'add',
			label: 'RENEWAL comes from POLICY',
			evidence: [],
			value: { eventType: 'RENEWAL', measure: '*', sourceSystem: 'POLICY', intakeMode: 'RAW_EVENT' }
		}
	],
	gaps: [],
	questions: ['Is stamp duty charged on home policies?'],
	notes: [],
	findings: [],
	problems: [],
	cases: [],
	balance: [],
	attempts: 1,
	usage: { promptTokens: 1000, completionTokens: 200 },
	...over
});

describe('turning a chosen proposal into content', () => {
	it('upserts by key, brings shared items only with a chosen event, and new classes only with a rule that uses them', () => {
		const p = proposal();
		expect(chosenItems(p, new Set())).toEqual([]);
		const items = chosenItems(p, new Set(['events/RENEWAL', 'sourceAuthority/RENEWAL']));
		expect(items.map((i) => i.id)).toEqual(['keyProfiles/INTERMEDIARY_ID', 'events/RENEWAL', 'sourceAuthority/RENEWAL']);
		const out = applyItems(content, items);
		expect((out.events as any[]).map((e) => e.code)).toContain('RENEWAL');
		expect((content.events as any[]).map((e) => e.code)).not.toContain('RENEWAL'); // a copy
		const again = applyItems(out, [{ section: 'events', key: 'RENEWAL', value: { code: 'RENEWAL', economicKey: ['POLICY_NO'], measures: ['PREMIUM'] } }]);
		expect((again.events as any[]).filter((e) => e.code === 'RENEWAL')).toHaveLength(1);

		const rules = proposal({
			step: 'rules',
			items: [
				{ id: 'accountingClasses/SD0', section: 'accountingClasses', key: 'SD0', group: '', change: 'add', label: 'New class SD0', evidence: [], value: { code: 'SD0', name: 'Stamp duty payable', nature: 'LIABILITY' } },
				{ id: 'accountingClasses/ZZ0', section: 'accountingClasses', key: 'ZZ0', group: '', change: 'add', label: 'New class ZZ0', evidence: [], value: { code: 'ZZ0', name: 'Unused', nature: 'ASSET' } },
				{ id: 'postingRules/PR_RENEWAL', section: 'postingRules', key: 'PR_RENEWAL', group: 'RENEWAL', change: 'add', label: 'PR_RENEWAL', evidence: [], value: { code: 'PR_RENEWAL', lines: [{ class: 'AR0' }, { class: 'SD0' }] } }
			]
		});
		expect(chosenItems(rules, new Set(['postingRules/PR_RENEWAL'])).map((i) => i.key)).toEqual(['SD0', 'PR_RENEWAL']);
	});

	it('turns an account number the person entered into an account and an assignment rule, and ignores invalid numbers', () => {
		const gaps = [
			{ kind: 'ACCOUNT_NUMBER' as const, code: 'SD0', name: 'Stamp duty payable', message: '' },
			{ kind: 'ACCOUNT_NUMBER' as const, code: 'CM0', name: 'Commission expense', message: '' },
			{ kind: 'ACCOUNT_NUMBER' as const, code: 'BP0', name: 'Broker payable', message: '' }
		];
		const out = gapItems(gaps, { SD0: { number: ' 2200300 ', name: '' }, CM0: { number: 'not valid!', name: 'x' } }, '2026-12-01', content);
		expect(out).toEqual([
			{ section: 'accounts', key: '2200300', value: { code: '2200300', name: 'Stamp duty payable', class: 'SD0', activeFrom: '2026-12-01' } },
			{ section: 'accountAssignmentRules', key: 'AA_SD0', value: { code: 'AA_SD0', class: 'SD0', account: '2200300', description: 'Every SD0 line posts to 2200300' } }
		]);
	});
});

function ctxFor(api: Record<string, any>, over: Partial<Ctx> = {}): Ctx {
	return {
		api: api as any,
		me: { tenantId: 'CLIENT_A', actor: 'ba@example.com', roles: ['CONFIG_EDITOR'] },
		version: {
			tenantId: 'CLIENT_A',
			configId: 'MOTOR_RENEWALS',
			version: 1,
			status: 'draft',
			effectiveFrom: '2026-12-01',
			effectiveTo: null,
			contentDigest: 'sha256:abc',
			createdBy: 'ba@example.com',
			createdAt: '',
			updatedBy: '',
			updatedAt: '',
			editors: ['ba@example.com']
		} as any,
		content,
		canEdit: true,
		isEditor: true,
		route: { configId: 'MOTOR_RENEWALS', version: 1, tab: 'ai' },
		go: vi.fn(),
		save: vi.fn(async () => {}),
		startDraft: vi.fn(),
		tick: 0,
		catalog: { measures: [], dimensions: [] },
		...over
	};
}

describe('configure from a BRD', () => {
	it('says when AI is not set up, and asks nothing', async () => {
		const api = { aiStatus: vi.fn(async () => ({ available: false, reason: 'AI is not set up on this server: no AI settings file is configured (GL_AI_SETTINGS).' })) };
		render(AIWizard, { ctx: ctxFor(api) });
		expect(await screen.findByText(/no AI settings file is configured/)).toBeTruthy();
		expect(screen.getByText('Not set up')).toBeTruthy();
	});

	it('reads the BRD, asks step 1, lets the person choose and saves only what they approve, with the AI origin', async () => {
		const api = {
			aiStatus: vi.fn(async () => ({ available: true, provider: 'openai', model: 'test-double' })),
			aiReadBRD: vi.fn(async (_id: string, _n: number, name: string) => ({ name, chars: 3456, text: '3.1 Renewal. When a policy is renewed…' })),
			aiStep: vi.fn(async () => ({ contentDigest: 'sha256:abc', proposal: proposal() })),
			check: vi.fn(async () => ({ ok: true, findings: [], contentDigest: 'sha256:x', changes: [{ section: 'events', key: 'RENEWAL', change: 'ADDED', fields: [] }] }))
		};
		const ctx = ctxFor(api);
		render(AIWizard, { ctx });
		await screen.findByText(/Ready:/);

		const file = new File(['# BRD\n3.1 Renewal'], 'motor-renewals-brd.md', { type: 'text/markdown' });
		await fireEvent.change(screen.getByLabelText('BRD file'), { target: { files: [file] } });
		await vi.waitFor(() => expect(api.aiReadBRD).toHaveBeenCalled());
		expect(api.aiReadBRD.mock.calls[0][2]).toBe('motor-renewals-brd.md');
		expect(await screen.findByText(/motor-renewals-brd\.md · 3,456 characters/)).toBeTruthy();

		await fireEvent.click(await screen.findByRole('button', { name: 'Ask the AI for step 1' }));
		await vi.waitFor(() => expect(api.aiStep).toHaveBeenCalled());
		expect((api.aiStep.mock.calls[0] as unknown[])[2]).toBe('events');
		expect(await screen.findByText('RENEWAL · Policy renewed')).toBeTruthy();
		expect(screen.getByText('not found in the BRD: check it')).toBeTruthy();
		expect(screen.getByText('Is stamp duty charged on home policies?')).toBeTruthy();

		// Nothing is chosen for the person.
		const approveBtn = screen.getByRole('button', { name: 'Approve and save to draft' }) as HTMLButtonElement;
		expect(approveBtn.disabled).toBe(true);
		expect(screen.getByText('Choose at least one item to approve.')).toBeTruthy();

		await fireEvent.click(screen.getByLabelText('Choose all of RENEWAL'));
		await vi.waitFor(() => expect(api.check).toHaveBeenCalled(), { timeout: 2000 });
		expect(approveBtn.disabled).toBe(false);
		await fireEvent.click(approveBtn);
		const dialog = await screen.findByRole('dialog');
		await fireEvent.input(within(dialog).getByRole('textbox'), { target: { value: 'Step 1 events from the BRD' } });
		await fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
		await vi.waitFor(() => expect(ctx.save).toHaveBeenCalled());

		const [saved, reason, origin] = (ctx.save as any).mock.calls[0];
		expect(reason).toBe('Step 1 events from the BRD');
		expect(origin).toEqual({ kind: 'AI_STEP', step: 'events', provider: 'openai', model: 'test-double', brd: 'motor-renewals-brd.md' });
		expect((saved.events as any[]).find((e) => e.code === 'RENEWAL')?.keys).toEqual(['POLICY_NO', 'TERM_NO', 'INTERMEDIARY_ID']);
		expect((saved.sourceAuthority as any[]).some((a) => a.eventType === 'RENEWAL' && a.sourceSystem === 'POLICY')).toBe(true);
		expect((saved.keyProfiles as any[]).some((k) => k.code === 'INTERMEDIARY_ID')).toBe(true);

		// Step 2 opens once step 1 is approved.
		const step2 = (await screen.findByRole('button', { name: /2\. Measures/ })) as HTMLButtonElement;
		expect(step2.disabled).toBe(false);
		expect(await screen.findByRole('button', { name: 'Ask the AI for step 2' })).toBeTruthy();
	});
});
