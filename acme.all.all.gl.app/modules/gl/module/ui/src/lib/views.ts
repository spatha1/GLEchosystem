import type { VersionTab } from './route';

/** The views of a configuration version: the side navigation lists them and each page heads with its label and purpose. */
export interface VersionView {
	key: VersionTab;
	label: string;
	short: string; // under the icon in the narrow rail
	icon: string;
	purpose: string;
}

export const versionViews: VersionView[] = [
	{
		key: 'events',
		label: 'Business events',
		short: 'Events',
		icon: 'events',
		purpose:
			'The business events this version configures. Select one to see what it is, where its data comes from, how its measures are calculated, which rules post it, which source fields it needs, and to simulate it.'
	},
	{
		key: 'ai',
		label: 'Configure from a BRD',
		short: 'AI',
		icon: 'ai',
		purpose:
			'Upload a business requirements document. The AI proposes the business events, then the measures, then the formulas, then the debit and credit lines with test cases, and you approve each step before it is saved to this draft.'
	},
	{
		key: 'fields',
		label: 'Required fields',
		short: 'Fields',
		icon: 'fields',
		purpose: 'The source fields this version needs from the client’s systems, and why each one is needed.'
	},
	{
		key: 'mapping',
		label: 'Data Workbench mapping',
		short: 'Mapping',
		icon: 'mapping',
		purpose: 'Where each required field is found in the client’s sources, through the Data Workbench.'
	},
	{
		key: 'simulate',
		label: 'Simulation',
		short: 'Simulate',
		icon: 'simulate',
		purpose: 'Run an event through this version’s rules on sample values, and see the journal it posts and where each line comes from.'
	},
	{
		key: 'impact',
		label: 'Change impact',
		short: 'Impact',
		icon: 'impact',
		purpose: 'What changes from an earlier version: events, rules, accounts, required fields and scenario results.'
	},
	{
		key: 'accounts',
		label: 'Chart of accounts',
		short: 'Accounts',
		icon: 'accounts',
		purpose:
			'Posting rules name accounting classes; each class is served by the client’s accounts; assignment rules choose the account for each line by dimension, priority and date. A line with no matching account, or two equally good ones, is refused — never guessed.'
	},
	{
		key: 'history',
		label: 'Findings and history',
		short: 'History',
		icon: 'history',
		purpose: 'Validation findings, maker-checker sign-offs and the audit trail of this version.'
	}
];

export const viewOf = (key: VersionTab | undefined) => versionViews.find((v) => v.key === (key ?? 'events')) ?? versionViews[0];
