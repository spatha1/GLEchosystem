// ALTB-08: the GL administrator manages the client's people and roles in the tool.
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '../lib/types';
import { fromHash, toHash } from '../lib/route';
import { cleanItem, specs } from '../lib/sections';
import RolesPage from './RolesPage.svelte';

afterEach(cleanup);

const admin: Me = { tenantId: 'NEW_CLIENT', actor: 'admin@example.com', roles: ['GL_ADMIN'] };

function apiWith(over: Record<string, unknown> = {}) {
	return {
		roles: vi.fn(async () => [{ actor: 'admin@example.com', roles: [{ role: 'GL_ADMIN', grantedBy: 'platform', grantedAt: '2026-11-22T08:00:00Z' }] }]),
		roleAudit: vi.fn(async () => [{ auditId: 1, actor: 'platform', subject: 'admin@example.com', granted: ['GL_ADMIN'], revoked: null, reason: 'client onboarding', at: '2026-11-22T08:00:00Z' }]),
		setRoles: vi.fn(async () => ({ actor: 'ba@example.com', roles: ['CONFIG_EDITOR'], changed: true })),
		...over
	} as any;
}

describe('roles', () => {
	it('has its own route', () => {
		expect(toHash({ admin: 'roles' })).toBe('#/admin/roles');
		expect(fromHash('#/admin/roles')).toEqual({ admin: 'roles' });
	});

	it('lists the people of the client with their roles and the role history', async () => {
		render(RolesPage, { api: apiWith(), me: admin });
		const row = (await screen.findAllByText('admin@example.com', { selector: 'td' }))[0].closest('tr')!;
		expect(within(row).getByText('GL administrator')).toBeTruthy();
		expect(within(row).getByText('you')).toBeTruthy();
		const history = screen.getByRole('region', { name: 'Role history' });
		expect(within(history).getByText('client onboarding')).toBeTruthy();
	});

	it('adds a person with roles and a reason; nothing is ticked until chosen', async () => {
		const api = apiWith();
		render(RolesPage, { api, me: admin });
		await fireEvent.click(await screen.findByRole('button', { name: '+ Add a person' }));
		const form = screen.getByRole('form', { name: 'Add a person' });
		for (const cb of within(form).getAllByRole('checkbox')) expect((cb as HTMLInputElement).checked).toBe(false);
		const save = within(form).getByRole('button', { name: 'Remove all roles' }) as HTMLButtonElement;
		expect(save.disabled).toBe(true);
		await fireEvent.input(within(form).getByLabelText(/Person/), { target: { value: 'BA@Example.com' } });
		await fireEvent.click(within(form).getByRole('checkbox', { name: /Business analyst/ }));
		expect((within(form).getByRole('button', { name: 'Save roles' }) as HTMLButtonElement).disabled).toBe(true); // a reason is needed
		await fireEvent.input(within(form).getByLabelText(/Reason/), { target: { value: 'Configures the client' } });
		await fireEvent.click(within(form).getByRole('button', { name: 'Save roles' }));
		await vi.waitFor(() => expect(api.setRoles).toHaveBeenCalledWith('ba@example.com', ['CONFIG_EDITOR'], 'Configures the client'));
	});

	it('shows why a change is refused, e.g. an administrator changing their own roles', async () => {
		const api = apiWith({
			setRoles: vi.fn(async () => {
				throw { code: 'SELF_ROLE_CHANGE', message: 'you cannot change your own roles; ask another administrator' };
			})
		});
		render(RolesPage, { api, me: admin });
		const row = (await screen.findAllByText('admin@example.com', { selector: 'td' }))[0].closest('tr')!;
		await fireEvent.click(within(row).getByRole('button', { name: 'Change' }));
		const form = screen.getByRole('form', { name: 'Roles of admin@example.com' });
		await fireEvent.click(within(form).getByRole('checkbox', { name: /Viewer/ }));
		await fireEvent.input(within(form).getByLabelText(/Reason/), { target: { value: 'also read' } });
		await fireEvent.click(within(form).getByRole('button', { name: 'Save roles' }));
		expect((await within(form).findByRole('alert')).textContent).toMatch(/SELF_ROLE_CHANGE/);
	});
});

describe('dimensions that take any value', () => {
	it('are saved as open, without a list of values', () => {
		expect(cleanItem(specs.dimensions.fields, { code: 'COST_CODE', open: true, values: [] })).toEqual({ code: 'COST_CODE', open: true });
		expect(specs.dimensions.columns.find((c) => c.label === 'Values')!.value({ code: 'X', open: true }, {})).toBe('any value');
	});
});
