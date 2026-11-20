// Display formatting. Amounts arrive as exact decimal strings and are only regrouped here —
// never converted to floating point.

/** "1099.50" → "1,099.50"; "-20.00" → "-20.00"; keeps every decimal as sent. */
export function amount(s: string): string {
	const m = /^(-?)(\d+)(\.\d+)?$/.exec(s.trim());
	if (!m) return s;
	const grouped = m[2].replace(/\B(?=(\d{3})+(?!\d))/g, ',');
	return `${m[1]}${grouped}${m[3] ?? ''}`;
}

/** "2026-01-10T09:15:02.123Z" → "2026-01-10 09:15" (UTC, as stored). */
export function when(iso: string | undefined | null): string {
	if (!iso) return '';
	const m = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2})/.exec(iso);
	return m ? `${m[1]} ${m[2]}` : iso;
}

/** "sha256:abcdef0123…" → "abcdef01". */
export function digest(d: string): string {
	return d.replace(/^sha256:/, '').slice(0, 8);
}

/** "AR0:P1" style keys for compact display. */
export function keyList(keys: Record<string, string>): string {
	return Object.keys(keys)
		.sort()
		.map((k) => `${k} ${keys[k]}`)
		.join(' · ');
}
