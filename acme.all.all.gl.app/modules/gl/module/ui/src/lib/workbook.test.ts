// ALTB-11 — import and export of a configuration as tables (Excel workbook, CSV, JSON).
// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import type { Content } from './studio';
import fixtureConfig from '../../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json';
import { importTables, same, tablesFromJson, tablesFromSheets, toTables, toWorkbook, type Table } from './workbook';
import { readCsv, readXlsx, unzip, writeCsv, writeXlsx, zip } from './sheets';

const content = structuredClone(fixtureConfig) as unknown as Content;

/** A fuller content: every kind of column and nested list. */
const rich: Content = {
	...structuredClone(content),
	journalTypes: [{ code: 'RJ', description: 'Reversing, next period', reverse: 'NEXT_PERIOD' }],
	outputLayouts: [
		{
			code: 'L1',
			recordLength: 40,
			fields: [
				{ name: 'ACCOUNT', start: 1, length: 10, source: 'ACCOUNT' },
				{ name: 'T2', start: 11, length: 5, source: 'DIMENSION', refs: ['STATE'], lengths: [2, 3] }
			]
		}
	],
	outputProfiles: [
		{
			code: 'P1',
			format: 'FIXED_WIDTH',
			layout: 'L1',
			fileName: 'F-{TO:yyMMdd}.txt',
			header: true,
			sections: [
				{ accounts: ['100'], group: 'LINE', dailyOffset: 'MONTH_END', offsetOverrides: [{ name: 'DESCRIPTION', source: 'CONSTANT', value: 'Bank | reversal' }] },
				{ excludeAccounts: ['100'], group: 'SUMMARY', groupBy: ['ACCOUNT', 'DIM:STATE'] }
			]
		}
	],
	lifecycleProfiles: [{ code: 'P_ONE', description: 'one, two', steps: ['S1', 'S2'] }],
	reconciliationEquations: [{ code: 'W1', size: 3, tags: ['a'], note: 'kept as it is' }]
};

describe('a configuration as tables', () => {
	it('round-trips every section, nested list and unknown section through tables', () => {
		const tables = toTables(rich);
		const names = tables.map((t) => t.name);
		expect(names).toContain('postingRules.lines');
		expect(names).toContain('outputProfiles.sections.offsetOverrides');
		expect(names).toContain('reconciliationEquations');
		const back = importTables({}, tables);
		expect(back.problems).toEqual([]);
		for (const s of Object.keys(rich)) expect(same(back.content[s], rich[s]), s).toBe(true);
		// and importing the same tables into the same content changes nothing
		const again = importTables(rich, tables);
		expect(again.sections.every((s) => !s.added.length && !s.changed.length && !s.removed.length)).toBe(true);
	});

	it('writes lists, pairs and flags so that a person can type them', () => {
		const rules = toTables(rich).find((t) => t.name === 'postingRules')!;
		expect(rules.header.slice(0, 3)).toEqual(['code', 'eventType', 'rank']);
		const rows = toTables(rich).find((t) => t.name === 'postingRules.lines')!;
		expect(rows.header[0]).toBe('parent');
		const prof = toTables(rich).find((t) => t.name === 'lifecycleProfiles')!;
		expect(prof.rows[0]).toEqual(['P_ONE', 'one, two', 'S1 | S2']);
		const offs = toTables(rich).find((t) => t.name === 'outputProfiles.sections.offsetOverrides')!;
		expect(offs.rows[0][0]).toBe('P1#1'); // the first section of P1
	});

	it('adds and updates by code, keeps what the file does not have, and replaces a list it has', () => {
		const t: Table[] = [
			{ name: 'accounts', header: ['code', 'name'], rows: [[content.accounts[0].code, 'Renamed'], ['NEW1', 'A new account']] },
			{ name: 'postingRules.lines', header: ['parent', 'code', 'side', 'class', 'amount'], rows: [[content.postingRules[0].code, 'ONLY', 'D', 'X', 'GROSS']] }
		];
		const r = importTables(content, t);
		expect(r.problems).toEqual([]);
		const acc = r.sections.find((s) => s.section === 'accounts')!;
		expect(acc.added).toEqual(['NEW1']);
		expect(acc.changed).toEqual([content.accounts[0].code]);
		expect(acc.unchanged).toBe(content.accounts.length - 1);
		const first = r.content.accounts[0];
		expect(first.name).toBe('Renamed');
		expect(first.class).toBe(content.accounts[0].class); // a column the sheet does not have is kept
		expect(r.content.postingRules[0].lines).toEqual([{ code: 'ONLY', side: 'D', class: 'X', amount: 'GROSS' }]);
		expect(r.content.postingRules[1]).toEqual(content.postingRules[1]); // other rules untouched
		expect(content.accounts[0].name).not.toBe('Renamed'); // the content given is never changed
	});

	it('in replace mode, a section in the file holds exactly the file’s items', () => {
		const t: Table[] = [{ name: 'accounts', header: ['code', 'name', 'class'], rows: [['ONLY1', 'Only', 'AR0']] }];
		const r = importTables(content, t, 'replace');
		expect(r.content.accounts).toEqual([{ code: 'ONLY1', name: 'Only', class: 'AR0' }]);
		expect(r.sections[0].removed.length).toBe(content.accounts.length);
		expect(r.content.postingRules).toEqual(content.postingRules); // other sections untouched
	});

	it('reports what it cannot read, and changes nothing then', () => {
		const t: Table[] = [
			{ name: 'acounts', header: ['code'], rows: [['X']] },
			{ name: 'postingRules', header: ['code', 'rank'], rows: [['R1', 'ten'], ['', '1'], ['R2', '1'], ['R2', '2']] },
			{ name: 'postingRules.lines', header: ['parent', 'code'], rows: [['NOPE', 'L1']] },
			{ name: 'outputLayouts.fields', header: ['code'], rows: [['F']] },
			{ name: 'calendars', header: ['code', 'holidays'], rows: [['C1', '2026-13-01x']] },
			{ name: 'datePolicies', header: ['code', 'fiscalYearStartMonth', 'advance'], rows: [['D1', '7', 'maybe']] },
			{ name: 'scenarios', header: ['code', 'input.keys', 'expect:json'], rows: [['S1', 'POLICY_NO', '{bad']] }
		];
		const p = importTables(content, t).problems.join('\n');
		expect(p).toMatch(/acounts: not a table/);
		expect(p).toMatch(/postingRules row 2, rank: "ten" is not a number/);
		expect(p).toMatch(/postingRules row 3: no code/);
		expect(p).toMatch(/R2 is in the sheet twice/);
		expect(p).toMatch(/"NOPE" is not an item of postingRules/);
		expect(p).toMatch(/outputLayouts.fields: the first column must be "parent"/);
		expect(p).toMatch(/advance: "maybe" is not true or false/);
		expect(p).toMatch(/"POLICY_NO" is not written NAME=value/);
		expect(p).toMatch(/expect: not valid JSON/);
	});

	it('reads Excel dates and flags as people type them', () => {
		const t: Table[] = [
			{ name: 'accounts', header: ['code', 'name', 'class', 'activeFrom'], rows: [['A9', 'Nine', 'AR0', '46204']] },
			{ name: 'postingRules', header: ['code', 'eventType', 'rank', 'active'], rows: [['R9', 'E', '5', 'FALSE']] }
		];
		const r = importTables({}, t);
		expect(r.problems).toEqual([]);
		expect(r.content.accounts[0].activeFrom).toBe('2026-07-01');
		expect(r.content.postingRules[0].active).toBe(false);
	});

	it('reads a JSON file: a version, a content or some sections', () => {
		const one = tablesFromJson(JSON.stringify({ configId: 'X', version: 2, content: { accounts: rich.accounts } }));
		expect(one.tables.map((t) => t.name)).toEqual(['accounts']);
		expect(tablesFromJson('{"accounts": 3}').error).toMatch(/expected a configuration/);
		expect(tablesFromJson('{').error).toMatch(/not valid JSON/);
	});
});

describe('files', () => {
	it('round-trips a configuration through an Excel workbook', async () => {
		const book = toWorkbook(rich);
		expect(book[0].name).toBe('Contents');
		expect(book[1].name).toBe('Columns');
		expect(book.every((s) => s.name.length <= 31)).toBe(true);
		const bytes = writeXlsx(book);
		const files = await unzip(bytes);
		expect([...files.keys()]).toContain('xl/workbook.xml');
		const sheets = await readXlsx(bytes);
		expect(sheets.map((s) => s.name)).toEqual(book.map((s) => s.name));
		const r = importTables({}, tablesFromSheets(sheets));
		expect(r.problems).toEqual([]);
		expect(r.ignored).toEqual(['Contents', 'Columns']);
		for (const s of Object.keys(rich)) expect(same(r.content[s], rich[s]), s).toBe(true);
	});

	it('reads a workbook saved by Excel: compressed, shared strings, numbers and booleans', async () => {
		const enc = new TextEncoder();
		const deflate = async (s: string) => {
			const bytes = enc.encode(s);
			const stream = new ReadableStream<Uint8Array>({
				start(c) {
					c.enqueue(bytes);
					c.close();
				}
			}).pipeThrough(new CompressionStream('deflate-raw') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>);
			const parts: Uint8Array[] = [];
			const reader = stream.getReader();
			for (let r = await reader.read(); !r.done; r = await reader.read()) parts.push(r.value);
			const out = new Uint8Array(parts.reduce((n, x) => n + x.length, 0));
			parts.reduce((p, x) => (out.set(x, p), p + x.length), 0);
			return out;
		};
		const NS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
		const parts: Record<string, string> = {
			'xl/workbook.xml': `<x:workbook xmlns:x="${NS}" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><x:sheets><x:sheet name="accounts" sheetId="1" r:id="rId1"/></x:sheets></x:workbook>`,
			'xl/_rels/workbook.xml.rels': `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="x" Target="/xl/worksheets/sheet1.xml"/></Relationships>`,
			'xl/sharedStrings.xml': `<sst xmlns="${NS}"><si><t>code</t></si><si><t>name</t></si><si><r><t>Cash </t></r><r><t>at bank</t></r><rPh><t>x</t></rPh></si><si><t>custom</t></si></sst>`,
			'xl/worksheets/sheet1.xml': `<worksheet xmlns="${NS}"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="D1" t="s"><v>3</v></c></row><row r="3"><c r="A3"><v>100000</v></c><c r="B3" t="s"><v>2</v></c><c r="D3" t="b"><v>1</v></c></row></sheetData></worksheet>`
		};
		// a zip with deflated entries, as Excel writes it
		const stored = zip(await Promise.all(Object.entries(parts).map(async ([name, xml]) => ({ name, data: await deflate(xml) }))));
		const v = new DataView(stored.buffer);
		for (let p = 0; p < stored.length - 4; p++) {
			if (v.getUint32(p, true) === 0x04034b50) v.setUint16(p + 8, 8, true);
			if (v.getUint32(p, true) === 0x02014b50) v.setUint16(p + 10, 8, true);
		}
		const sheets = await readXlsx(stored);
		expect(sheets[0].rows).toEqual([['code', 'name', '', 'custom'], [], ['100000', 'Cash at bank', '', 'true']]);
		const t = tablesFromSheets(sheets);
		expect(t[0].rows).toEqual([['100000', 'Cash at bank', '', 'true']]); // empty rows skipped
	});

	it('writes and reads CSV, with quotes, commas, line breaks and a semicolon separator', () => {
		const rows = [['code', 'name'], ['A1', 'Cash, "petty"\nand more'], ['A2', ' padded ']];
		expect(readCsv(writeCsv(rows))).toEqual(rows);
		expect(readCsv('code;name\r\nA1;Cash\r\n')).toEqual([['code', 'name'], ['A1', 'Cash']]);
	});
});
