// What every Stage 1 step needs.
import type { Content, VersionFull } from '../../lib/studio';
import type { Me } from '../../lib/types';
import type { ClassLibrary, WsClient } from '../api';
import type { LoadedFile } from '../files';

export interface S1 {
	api: WsClient;
	me: Me;
	version: VersionFull;
	content: Content;
	/** a draft (or validated) version and the person edits configurations */
	canEdit: boolean;
	isEditor: boolean;
	isApprover: boolean;
	library: ClassLibrary;
	/** the client's source systems, from onboarding */
	systems: string[];
	/** files brought in this session, shared by the steps and the assistant */
	files: LoadedFile[];
	addFiles: (f: LoadedFile[]) => void;
	/** saves content to the draft as one change with a reason (the server checks it) */
	save: (content: Content, reason: string) => Promise<void>;
	reload: () => Promise<void>;
	step: (n: number) => void;
}
