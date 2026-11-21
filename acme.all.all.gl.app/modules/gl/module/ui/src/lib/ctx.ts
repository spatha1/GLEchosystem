// What every screen inside a version needs: the version (with its content), who is
// looking, whether they may edit it, and how to save, reload and navigate.
import type { Client, Origin } from './api';
import type { Me } from './types';
import type { Content, VersionFull } from './studio';
import type { Route } from './route';
import type { Catalog } from './sections';

export interface Ctx {
	api: Client;
	me: Me;
	version: VersionFull;
	content: Content;
	/** Draft or validated, and the user holds CONFIG_EDITOR. */
	canEdit: boolean;
	/** The user holds CONFIG_EDITOR (and so may start a draft from this version). */
	isEditor: boolean;
	route: Route;
	go: (r: Route) => void;
	/** Saves edited content to this draft (PUT with the digest it was read at); origin marks an approved AI proposal. */
	save: (content: Content, reason: string, origin?: Origin) => Promise<void>;
	/** Opens the "new draft from this version" dialog; then navigates to `then` in the draft. */
	startDraft: (then?: Route) => void;
	/** Increments whenever the version is reloaded, so views refetch. */
	tick: number;
	/** The standard measure and dimension catalogs, for the editors' choices. */
	catalog: Catalog;
}

export function editable(v: VersionFull, me: Me): boolean {
	return (v.status === 'draft' || v.status === 'validated') && me.roles.includes('CONFIG_EDITOR');
}
