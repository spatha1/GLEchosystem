// What the signed-in user may do with a version. The server decides (US-004 roles and the
// database's maker-checker constraint); the screen only explains, so a disabled action
// always says why instead of failing after a click.
import type { Me, Version } from './types';

export type ActionKind = 'validate' | 'approve' | 'retire';

export interface Action {
	kind: ActionKind;
	label: string;
	enabled: boolean;
	why?: string;
}

export function actionsFor(v: Version, me: Me): Action[] {
	const has = (r: string) => me.roles.includes(r as never);
	const out: Action[] = [];
	if (v.status === 'draft') {
		out.push(
			has('CONFIG_EDITOR')
				? { kind: 'validate', label: 'Validate', enabled: true }
				: { kind: 'validate', label: 'Validate', enabled: false, why: 'Needs the CONFIG_EDITOR role.' }
		);
	}
	if (v.status === 'validated') {
		if (!has('CONFIG_APPROVER')) {
			out.push({ kind: 'approve', label: 'Approve', enabled: false, why: 'Needs the CONFIG_APPROVER role.' });
		} else if (v.editors.includes(me.actor)) {
			out.push({ kind: 'approve', label: 'Approve', enabled: false, why: 'You changed this version, so another approver must approve it (maker-checker).' });
		} else {
			out.push({ kind: 'approve', label: 'Approve', enabled: true });
		}
	}
	if (v.status === 'validated' || v.status === 'approved') {
		out.push(
			has('CONFIG_APPROVER')
				? { kind: 'retire', label: 'Retire', enabled: true }
				: { kind: 'retire', label: 'Retire', enabled: false, why: 'Needs the CONFIG_APPROVER role.' }
		);
	}
	return out;
}
