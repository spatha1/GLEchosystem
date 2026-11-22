// ALTB-11 — reading and writing the files of an import: Excel workbooks (.xlsx) and CSV. No
// dependency: an .xlsx file is a zip of XML parts; it is written uncompressed and read with the
// browser's own DecompressionStream. Every cell is written as text, so codes such as 001 or
// 100000 come back exactly as they were.

export interface Sheet {
	name: string;
	rows: string[][];
}

// ── zip ──────────────────────────────────────────────────────────────────────────────────────

const CRC = (() => {
	const t = new Uint32Array(256);
	for (let n = 0; n < 256; n++) {
		let c = n;
		for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
		t[n] = c >>> 0;
	}
	return t;
})();

function crc32(b: Uint8Array): number {
	let c = 0xffffffff;
	for (let i = 0; i < b.length; i++) c = CRC[(c ^ b[i]) & 0xff] ^ (c >>> 8);
	return (c ^ 0xffffffff) >>> 0;
}

/** A zip of the files, stored (not compressed). */
export function zip(files: { name: string; data: Uint8Array }[]): Uint8Array {
	const enc = new TextEncoder();
	const parts: Uint8Array[] = [];
	const central: Uint8Array[] = [];
	let offset = 0;
	const DATE = (0 << 9) | (1 << 5) | 1; // 1980-01-01: the same bytes for the same content
	for (const f of files) {
		const name = enc.encode(f.name);
		const crc = crc32(f.data);
		const local = new DataView(new ArrayBuffer(30));
		local.setUint32(0, 0x04034b50, true);
		local.setUint16(4, 20, true);
		local.setUint16(6, 0x0800, true); // UTF-8 names
		local.setUint16(8, 0, true); // stored
		local.setUint16(10, 0, true);
		local.setUint16(12, DATE, true);
		local.setUint32(14, crc, true);
		local.setUint32(18, f.data.length, true);
		local.setUint32(22, f.data.length, true);
		local.setUint16(26, name.length, true);
		local.setUint16(28, 0, true);
		parts.push(new Uint8Array(local.buffer), name, f.data);
		const cd = new DataView(new ArrayBuffer(46));
		cd.setUint32(0, 0x02014b50, true);
		cd.setUint16(4, 20, true);
		cd.setUint16(6, 20, true);
		cd.setUint16(8, 0x0800, true);
		cd.setUint16(10, 0, true);
		cd.setUint16(12, 0, true);
		cd.setUint16(14, DATE, true);
		cd.setUint32(16, crc, true);
		cd.setUint32(20, f.data.length, true);
		cd.setUint32(24, f.data.length, true);
		cd.setUint16(28, name.length, true);
		cd.setUint32(42, offset, true);
		central.push(new Uint8Array(cd.buffer), name);
		offset += 30 + name.length + f.data.length;
	}
	const cdSize = central.reduce((n, b) => n + b.length, 0);
	const end = new DataView(new ArrayBuffer(22));
	end.setUint32(0, 0x06054b50, true);
	end.setUint16(8, files.length, true);
	end.setUint16(10, files.length, true);
	end.setUint32(12, cdSize, true);
	end.setUint32(16, offset, true);
	const all = [...parts, ...central, new Uint8Array(end.buffer)];
	const out = new Uint8Array(all.reduce((n, b) => n + b.length, 0));
	let p = 0;
	for (const b of all) {
		out.set(b, p);
		p += b.length;
	}
	return out;
}

async function inflateRaw(b: Uint8Array): Promise<Uint8Array> {
	const stream = new ReadableStream<Uint8Array>({
		start(c) {
			c.enqueue(b);
			c.close();
		}
	}).pipeThrough(new DecompressionStream('deflate-raw') as unknown as ReadableWritablePair<Uint8Array, Uint8Array>);
	const chunks: Uint8Array[] = [];
	const reader = stream.getReader();
	for (;;) {
		const { done, value } = await reader.read();
		if (done) break;
		chunks.push(value);
	}
	const out = new Uint8Array(chunks.reduce((n, x) => n + x.length, 0));
	let p = 0;
	for (const x of chunks) {
		out.set(x, p);
		p += x.length;
	}
	return out;
}

/** The files of a zip, by name (stored or deflated entries). */
export async function unzip(bytes: Uint8Array): Promise<Map<string, Uint8Array>> {
	const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
	let e = bytes.length - 22;
	while (e >= 0 && v.getUint32(e, true) !== 0x06054b50) e--;
	if (e < 0) throw new Error('not a zip file (is it an .xlsx workbook?)');
	const count = v.getUint16(e + 10, true);
	let p = v.getUint32(e + 16, true);
	const dec = new TextDecoder();
	const out = new Map<string, Uint8Array>();
	for (let i = 0; i < count; i++) {
		if (v.getUint32(p, true) !== 0x02014b50) throw new Error('damaged zip file');
		const method = v.getUint16(p + 10, true);
		const size = v.getUint32(p + 20, true);
		const nameLen = v.getUint16(p + 28, true);
		const extraLen = v.getUint16(p + 30, true);
		const commentLen = v.getUint16(p + 32, true);
		const local = v.getUint32(p + 42, true);
		const name = dec.decode(bytes.subarray(p + 46, p + 46 + nameLen));
		const start = local + 30 + v.getUint16(local + 26, true) + v.getUint16(local + 28, true);
		const data = bytes.subarray(start, start + size);
		if (method === 0) out.set(name, data);
		else if (method === 8) out.set(name, await inflateRaw(data));
		else throw new Error(`${name}: compression method ${method} is not supported`);
		p += 46 + nameLen + extraLen + commentLen;
	}
	return out;
}

// ── xlsx ─────────────────────────────────────────────────────────────────────────────────────

const NS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const RNS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const esc = (s: string) =>
	s
		.replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g, '')
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;');

export function colName(i: number): string {
	let s = '';
	for (let n = i + 1; n > 0; n = Math.floor((n - 1) / 26)) s = String.fromCharCode(65 + ((n - 1) % 26)) + s;
	return s;
}

function colIndex(ref: string): number {
	let n = 0;
	for (const ch of ref.replace(/[0-9]/g, '')) n = n * 26 + (ch.charCodeAt(0) - 64);
	return n - 1;
}

/** An Excel workbook: one sheet per entry, the first row bold and frozen, every cell text. */
export function writeXlsx(sheets: Sheet[]): Uint8Array {
	const enc = new TextEncoder();
	const files: { name: string; data: Uint8Array }[] = [];
	const add = (name: string, xml: string) => files.push({ name, data: enc.encode('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n' + xml) });
	add(
		'[Content_Types].xml',
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>${sheets
			.map((_, i) => `<Override PartName="/xl/worksheets/sheet${i + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`)
			.join('')}</Types>`
	);
	add('_rels/.rels', `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="${RNS}/officeDocument" Target="xl/workbook.xml"/></Relationships>`);
	add(
		'xl/workbook.xml',
		`<workbook xmlns="${NS}" xmlns:r="${RNS}"><sheets>${sheets.map((s, i) => `<sheet name="${esc(s.name)}" sheetId="${i + 1}" r:id="rId${i + 1}"/>`).join('')}</sheets></workbook>`
	);
	add(
		'xl/_rels/workbook.xml.rels',
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${sheets
			.map((_, i) => `<Relationship Id="rId${i + 1}" Type="${RNS}/worksheet" Target="worksheets/sheet${i + 1}.xml"/>`)
			.join('')}<Relationship Id="rId${sheets.length + 1}" Type="${RNS}/styles" Target="styles.xml"/></Relationships>`
	);
	// styles: 0 normal, 1 bold (header), 2 text format (so what is typed stays text)
	add(
		'xl/styles.xml',
		`<styleSheet xmlns="${NS}"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="49" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyNumberFormat="1"/><xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
	);
	sheets.forEach((s, i) => {
		const width = Math.max(1, ...s.rows.map((r) => r.length));
		const cols = Array.from({ length: width }, (_, c) => {
			const w = Math.min(60, Math.max(8, ...s.rows.slice(0, 200).map((r) => (r[c] ?? '').length + 2)));
			return `<col min="${c + 1}" max="${c + 1}" width="${w}" style="2" customWidth="1"/>`;
		}).join('');
		const rows = s.rows
			.map(
				(r, ri) =>
					`<row r="${ri + 1}">${r
						.map((v, ci) =>
							v === '' || v == null
								? ''
								: `<c r="${colName(ci)}${ri + 1}" t="inlineStr" s="${ri === 0 ? 1 : 2}"><is><t xml:space="preserve">${esc(String(v))}</t></is></c>`
						)
						.join('')}</row>`
			)
			.join('');
		const frozen = s.rows.length ? '<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>' : '';
		add(`xl/worksheets/sheet${i + 1}.xml`, `<worksheet xmlns="${NS}">${frozen}<cols>${cols}</cols><sheetData>${rows}</sheetData></worksheet>`);
	});
	return zip(files);
}

function parseXml(bytes: Uint8Array | undefined, what: string): Document {
	if (!bytes) throw new Error(`the workbook has no ${what}`);
	const doc = new DOMParser().parseFromString(new TextDecoder().decode(bytes), 'application/xml');
	if (doc.getElementsByTagName('parsererror').length) throw new Error(`the workbook's ${what} cannot be read`);
	return doc;
}

const all = (n: Document | Element, local: string) => Array.from(n.getElementsByTagNameNS('*', local));
const attr = (e: Element, local: string) => Array.from(e.attributes).find((a) => a.localName === local)?.value ?? '';
/** Text of a string item, without phonetic runs. */
const text = (e: Element) =>
	all(e, 't')
		.filter((t) => !t.closest('rPh'))
		.map((t) => t.textContent ?? '')
		.join('');

/** The sheets of an Excel workbook, as rows of text (numbers as written, dates as serial numbers). */
export async function readXlsx(bytes: Uint8Array): Promise<Sheet[]> {
	const files = await unzip(bytes);
	const book = parseXml(files.get('xl/workbook.xml'), 'workbook');
	const rels = parseXml(files.get('xl/_rels/workbook.xml.rels'), 'relationships');
	const target = new Map(all(rels, 'Relationship').map((r) => [attr(r, 'Id'), attr(r, 'Target')]));
	const shared = files.has('xl/sharedStrings.xml') ? all(parseXml(files.get('xl/sharedStrings.xml'), 'shared strings'), 'si').map(text) : [];
	return all(book, 'sheet').map((s) => {
		let t = target.get(attr(s, 'id')) ?? '';
		t = t.startsWith('/') ? t.slice(1) : 'xl/' + t;
		const doc = parseXml(files.get(t), `sheet ${attr(s, 'name')}`);
		const rows: string[][] = [];
		for (const row of all(doc, 'row')) {
			const r: string[] = [];
			let next = 0;
			for (const c of all(row, 'c')) {
				const ref = attr(c, 'r');
				const ci = ref ? colIndex(ref) : next;
				next = ci + 1;
				const type = attr(c, 't');
				const v = all(c, 'v')[0]?.textContent ?? '';
				let val: string;
				if (type === 's') val = shared[Number(v)] ?? '';
				else if (type === 'inlineStr') val = all(c, 'is').map(text).join('');
				else if (type === 'b') val = v === '1' ? 'true' : 'false';
				else if (type === 'str' || type === 'e') val = v;
				else val = v !== '' && Number.isFinite(Number(v)) ? String(Number(v)) : v; // 0.1 rather than 0.10000000000000001
				while (r.length < ci) r.push('');
				r[ci] = val;
			}
			const rn = Number(attr(row, 'r')) || rows.length + 1;
			while (rows.length < rn - 1) rows.push([]);
			rows.push(r);
		}
		return { name: attr(s, 'name'), rows };
	});
}

// ── csv ──────────────────────────────────────────────────────────────────────────────────────

export function writeCsv(rows: string[][]): string {
	const cell = (v: string) => (/[",\r\n]|^\s|\s$/.test(v) ? `"${v.replace(/"/g, '""')}"` : v);
	return '﻿' + rows.map((r) => r.map((v) => cell(v ?? '')).join(',')).join('\r\n') + '\r\n';
}

/** Rows of a CSV file; the separator (comma, semicolon or tab) is taken from the first line. */
export function readCsv(text: string): string[][] {
	const s = text.replace(/^﻿/, '');
	const first = s.split(/\r?\n/)[0].replace(/"[^"]*"/g, '');
	const sep = [',', ';', '\t'].map((c) => [c, first.split(c).length] as const).sort((a, b) => b[1] - a[1])[0][0];
	const rows: string[][] = [];
	let row: string[] = [];
	let cur = '';
	let quoted = false;
	for (let i = 0; i < s.length; i++) {
		const ch = s[i];
		if (quoted) {
			if (ch === '"' && s[i + 1] === '"') {
				cur += '"';
				i++;
			} else if (ch === '"') quoted = false;
			else cur += ch;
		} else if (ch === '"') quoted = true;
		else if (ch === sep) {
			row.push(cur);
			cur = '';
		} else if (ch === '\n' || ch === '\r') {
			if (ch === '\r' && s[i + 1] === '\n') i++;
			row.push(cur);
			rows.push(row);
			row = [];
			cur = '';
		} else cur += ch;
	}
	if (cur !== '' || row.length) {
		row.push(cur);
		rows.push(row);
	}
	return rows;
}
