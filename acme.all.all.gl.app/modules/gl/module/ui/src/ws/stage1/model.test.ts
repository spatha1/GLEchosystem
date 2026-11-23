// Stage 1 logic: reading a client's chart, class list and assignment matrix, mapping source
// classes, coverage. A synthetic client; nothing here comes from a real client.
import { describe, expect, it } from 'vitest';
import type { Content } from '../../lib/studio';
import type { LoadedSheet } from '../files';
import { adopt, applyMatrix, coverage, detectColumns, detectKind, detectMatrix, importClasses, importCOA, mapSourceClass, overlaps, readMatrix, setMappings, COA_ROLES, CLASS_ROLES } from './model';
import { importTables, toTables } from '../../lib/workbook';

const coa: LoadedSheet = {
	file: 'chart.csv',
	sheet: '',
	header: ['account_code', 'description', 'derived_account_type', 'normal_balance', 'notes'],
	rows: [
		['100000', 'Bank', 'Asset', 'Debit', 'main account'],
		['120010', 'Debtors control', 'Asset', 'Debit', ''],
		['400000', 'Premium written', 'Income', 'Credit', ''],
		['230015', 'GST', 'Liability', 'Credit', ''],
		['', 'no number', '', '', ''],
		['120010', 'Debtors again', 'Asset', 'Debit', '']
	]
};
const classList: LoadedSheet = {
	file: 'classes.csv',
	sheet: '',
	header: ['accounting_class', 'meaning', 'role'],
	rows: [
		['AR0', 'Open receivable', 'Receivable created by the policy transaction'],
		['PW0', 'Premium and tax source', 'Credit side'],
		['BF2', 'Fee closed receivable', '']
	]
};
const matrix: LoadedSheet = {
	file: 'assignment.xlsx',
	sheet: 'Rules',
	header: ['AccountingClassCode', 'TransactionTypeCode', 'MasterCompanyCode', 'GLAccount', 'Priority'],
	rows: [
		['AR0', '*', 'C1', '120010', ''],
		['PW0', 'PREM', 'C1', '400000', '1'],
		['PW0', 'TAX', 'C1', '230015', '1'],
		['BF2', '*', 'C1', '120010', ''],
		['PW0', 'FEE', 'C1', '400000 / 510000', '']
	]
};

describe('reading the client’s files', () => {
	it('finds columns and what a sheet holds from its headers', () => {
		expect(detectKind(coa)).toBe('COA');
		expect(detectKind(classList)).toBe('CLASSES');
		expect(detectKind(matrix)).toBe('ASSIGNMENTS');
		// a class list whose headers mention accounts is still a class list
		expect(detectKind({ ...classList, header: ['accounting_class', 'meaning', 'role', 'gl_account_use'] })).toBe('CLASSES');
		expect(detectKind({ file: 'rules.csv', sheet: '', header: ['AccountingClassCode', 'ActivityTypeCode', 'GLAccount'], rows: [] })).toBe('ASSIGNMENTS');
		expect(detectColumns(coa.header, COA_ROLES)).toEqual({ account: 'account_code', name: 'description', type: 'derived_account_type', normalBalance: 'normal_balance' });
		expect(detectColumns(classList.header, CLASS_ROLES)).toEqual({ code: 'accounting_class', meaning: 'meaning', role: 'role' });
	});

	it('imports a chart with its source rows, skipping what it cannot read', () => {
		const p = importCOA({}, coa, detectColumns(coa.header, COA_ROLES), '2026-07-01', 'ba@x');
		expect(p.added).toEqual(['100000', '120010', '400000', '230015']);
		expect(p.skipped).toEqual([
			{ row: 5, reason: 'no account number' },
			{ row: 6, reason: 'account 120010 is in the sheet twice' }
		]);
		const bank = (p.content.accounts as any[])[0];
		expect(bank).toMatchObject({ code: '100000', name: 'Bank', type: 'ASSET', normalBalance: 'D', activeFrom: '2026-07-01', attributes: { notes: 'main account' } });
		expect(bank.source).toMatchObject({ row: 1, values: { account_code: '100000', description: 'Bank' } });
		expect(bank.class).toBeUndefined(); // classes come from steps 2 and 3
		// importing the same sheet again changes nothing
		const again = importCOA(p.content, coa, detectColumns(coa.header, COA_ROLES), '2026-07-01', 'ba@x');
		expect(again.added).toEqual([]);
		expect(again.changed).toEqual([]);
		expect(again.unchanged).toBe(4);
	});

	it('keeps source class codes exactly as received, and keeps mappings already made', () => {
		let c: Content = importClasses({}, classList, detectColumns(classList.header, CLASS_ROLES), 'PAS', 'ba@x').content;
		c = setMappings(c, 'PAS', 'AR0', [{ class: 'POLICY_RECEIVABLE' }]);
		const p = importClasses(c, classList, detectColumns(classList.header, CLASS_ROLES), 'PAS', 'ba@x');
		const ar0 = (p.content.sourceClasses as any[]).find((s) => s.code === 'AR0');
		expect(ar0.mappings).toEqual([{ class: 'POLICY_RECEIVABLE' }]);
		expect((p.content.sourceClasses as any[]).map((s) => s.code)).toEqual(['AR0', 'PW0', 'BF2']);
	});
});

const std = (code: string, nature: string) => ({ code, group: 'g', name: code, definition: 'd', nature, normalSide: 'D', status: 'PROPOSED' as const });

function mapped(): Content {
	let c: Content = importCOA({}, coa, detectColumns(coa.header, COA_ROLES), '2026-07-01', 'ba@x').content;
	c = importClasses(c, classList, detectColumns(classList.header, CLASS_ROLES), 'PAS', 'ba@x').content;
	adopt(c, std('POLICY_RECEIVABLE', 'ASSET'));
	adopt(c, std('PREMIUM_WRITTEN', 'INCOME'));
	adopt(c, std('TAX_PAYABLE', 'LIABILITY'));
	setMappings(c, 'PAS', 'AR0', [{ class: 'POLICY_RECEIVABLE' }]);
	setMappings(c, 'PAS', 'PW0', [
		{ class: 'PREMIUM_WRITTEN', when: { TRANSACTION_TYPE_CODE: 'PREM' } },
		{ class: 'TAX_PAYABLE', when: { TRANSACTION_TYPE_CODE: 'TAX' } }
	]);
	return c;
}

describe('classes and assignment rules', () => {
	it('maps a source class that means several things by its condition', () => {
		const c = mapped();
		expect(mapSourceClass(c, 'PAS', 'PW0', { TRANSACTION_TYPE_CODE: 'TAX' }).class).toBe('TAX_PAYABLE');
		expect(mapSourceClass(c, 'PAS', 'PW0', { TRANSACTION_TYPE_CODE: 'PREM' }).class).toBe('PREMIUM_WRITTEN');
		expect(mapSourceClass(c, 'PAS', 'PW0', { TRANSACTION_TYPE_CODE: 'FEE' }).why).toMatch(/no mapping/);
		expect((c.accountingClasses as any[])[0]).toMatchObject({ code: 'POLICY_RECEIVABLE', standard: 'POLICY_RECEIVABLE' });
	});

	it('reads a matrix: conditions, wildcards, priorities and lineage, never guessing a row', () => {
		const c = mapped();
		const cols = detectMatrix(matrix, 'PAS');
		expect(cols.sourceClass).toBe('AccountingClassCode');
		expect(cols.accounts.map((a) => a.header)).toEqual(['GLAccount']);
		expect(cols.conditions).toEqual({ TransactionTypeCode: 'TRANSACTION_TYPE_CODE', MasterCompanyCode: 'MASTER_COMPANY_CODE' });
		const p = readMatrix(c, matrix, cols, 'ba@x');
		expect(p.rules.map((r) => [r.class, r.match, r.account, r.priority])).toEqual([
			['POLICY_RECEIVABLE', { TRANSACTION_TYPE_CODE: '*', MASTER_COMPANY_CODE: 'C1' }, '120010', undefined],
			['PREMIUM_WRITTEN', { TRANSACTION_TYPE_CODE: 'PREM', MASTER_COMPANY_CODE: 'C1' }, '400000', 1],
			['TAX_PAYABLE', { TRANSACTION_TYPE_CODE: 'TAX', MASTER_COMPANY_CODE: 'C1' }, '230015', 1]
		]);
		expect(p.rules[1].source).toMatchObject({ row: 2, sheet: 'Rules', values: { AccountingClassCode: 'PW0', TransactionTypeCode: 'PREM', GLAccount: '400000' } });
		expect(p.skipped.map((s) => s.row)).toEqual([4, 5]);
		expect(p.skipped[0].reason).toMatch(/BF2: no mapping of BF2 applies|BF2.*map it in step 2/);
		expect(p.skipped[1].reason).toMatch(/no mapping of PW0 applies/);
		expect(p.newDimensions).toEqual(['TRANSACTION_TYPE_CODE', 'MASTER_COMPANY_CODE']);
		const after = applyMatrix(c, p);
		expect((after.dimensions as any[]).map((d) => [d.code, d.open, d.custom])).toEqual([
			['TRANSACTION_TYPE_CODE', true, true],
			['MASTER_COMPANY_CODE', true, true]
		]);
		// every row of every sheet is accounted for: imported, or skipped with its reason
		const cov = coverage(after);
		expect(cov.map((x) => [x.file.kind, x.imported.length, x.skipped.length, x.missing])).toEqual([
			['COA', 4, 2, []],
			['CLASSES', 3, 0, []],
			['ASSIGNMENTS', 3, 2, []]
		]);
		expect(overlaps(after)).toEqual([]);
		// re-reading the matrix replaces its rules instead of adding them twice
		expect(((applyMatrix(after, p).accountAssignmentRules ?? []) as any[]).length).toBe(3);
	});

	it('travels through the import and export workbook with its lineage', () => {
		const c = applyMatrix(mapped(), readMatrix(mapped(), matrix, detectMatrix(matrix, 'PAS'), 'ba@x'));
		const back = importTables({}, toTables(c));
		expect(back.problems, back.problems.join(' | ')).toEqual([]);
		expect(back.content.sourceClasses).toEqual(c.sourceClasses);
		expect((back.content.accountAssignmentRules as any[])[1].source).toEqual((c.accountAssignmentRules as any[])[1].source);
		expect((back.content.sourceFiles as any[]).map((f) => f.kind)).toEqual(['COA', 'CLASSES', 'ASSIGNMENTS']);
	});
});
