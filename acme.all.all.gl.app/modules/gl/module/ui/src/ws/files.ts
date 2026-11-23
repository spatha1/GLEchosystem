// Reading the files a person brings: Excel workbooks and CSV as sheets of rows; text documents
// (Markdown, text) as text. Nothing leaves the browser here.
import { readCsv, readXlsx, writeCsv } from '../lib/sheets';

export interface LoadedSheet {
	file: string;
	sheet: string;
	header: string[];
	rows: string[][]; // data rows, without the header
}

export interface LoadedFile {
	name: string;
	sheets: LoadedSheet[];
	text?: string; // a text document
}

function bytesOf(f: File): Promise<Uint8Array> {
	if (typeof f.arrayBuffer === 'function') return f.arrayBuffer().then((b) => new Uint8Array(b));
	return new Promise((ok, fail) => {
		const r = new FileReader();
		r.onload = () => ok(new Uint8Array(r.result as ArrayBuffer));
		r.onerror = () => fail(r.error);
		r.readAsArrayBuffer(f);
	});
}

const clean = (rows: string[][]) => rows.filter((r) => r.some((c) => (c ?? '').trim()));

export async function loadFile(f: File): Promise<LoadedFile> {
	const ext = f.name.toLowerCase().split('.').pop() ?? '';
	const bytes = await bytesOf(f);
	if (ext === 'xlsx') {
		const sheets = await readXlsx(bytes);
		return {
			name: f.name,
			sheets: sheets
				.map((s) => {
					const rows = clean(s.rows);
					return { file: f.name, sheet: s.name, header: (rows[0] ?? []).map((h) => (h ?? '').trim()), rows: rows.slice(1) };
				})
				.filter((s) => s.header.length)
		};
	}
	const text = new TextDecoder().decode(bytes);
	if (ext === 'csv' || ext === 'tsv') {
		const rows = clean(readCsv(text));
		return { name: f.name, sheets: [{ file: f.name, sheet: '', header: (rows[0] ?? []).map((h) => (h ?? '').trim()), rows: rows.slice(1) }] };
	}
	if (ext === 'txt' || ext === 'md' || ext === 'json') return { name: f.name, sheets: [], text };
	throw new Error(`${f.name}: choose an Excel workbook (.xlsx), a CSV file, or a text or Markdown document`);
}

/** A file as text for the assistant: each sheet as CSV, headed by its name. */
export function asDocuments(files: LoadedFile[]): { name: string; text: string }[] {
	return files.map((f) => ({
		name: f.name,
		text: f.text ?? f.sheets.map((s) => (s.sheet ? `# sheet ${s.sheet}\n` : '') + writeCsv([s.header, ...s.rows]).replace(/^﻿/, '')).join('\n')
	}));
}

export const sheetLabel = (s: LoadedSheet) => (s.sheet ? `${s.file} › ${s.sheet}` : s.file);

/** A short, stable id for an imported sheet. */
export function sheetId(s: LoadedSheet): string {
	let h = 0;
	for (const ch of `${s.file}|${s.sheet}|${s.header.join(',')}`) h = (Math.imul(h, 31) + ch.charCodeAt(0)) | 0;
	return 'F' + (h >>> 0).toString(36).toUpperCase();
}

/** UPPER_SNAKE code from a column header (ActivityTypeCode → ACTIVITY_TYPE_CODE). */
export function codeOf(header: string): string {
	const c = header
		.trim()
		.replace(/([a-z0-9])([A-Z])/g, '$1_$2')
		.replace(/[^A-Za-z0-9]+/g, '_')
		.replace(/^_+|_+$/g, '')
		.toUpperCase();
	return /^[A-Z]/.test(c) ? c.slice(0, 64) : ('C_' + c).slice(0, 64);
}
