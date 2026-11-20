// Seeds the local dev adapter (service/cmd/devhttp, loopback only) with the GL-TEST-001 demo
// configuration for tenant DEMO, through the API and its maker-checker flow:
//   v1 (from 2026-01-01): created and validated by ba@, approved by reviewer@
//   v2 (from 2026-02-01): created and validated by lead@ (editor and approver), awaiting a
//       second approver — lead@ sees Approve disabled, reviewer@ can approve.
// Requires GL_DEV_SEED_ROLES on devhttp for DEMO: ba@ CONFIG_EDITOR, reviewer@ CONFIG_APPROVER,
// lead@ CONFIG_EDITOR|CONFIG_APPROVER, viewer@ VIEWER.
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const API = (process.env.GL_DEV_API ?? 'http://127.0.0.1:9080') + '/m/gl';
const fixture = fileURLToPath(new URL('../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json', import.meta.url));
const content = JSON.parse(await readFile(fixture, 'utf8'));
const ID = 'DEMO_GL';

async function call(actor, method, path, body) {
	const res = await fetch(API + path, {
		method,
		headers: { 'x-mycel-user': actor, 'x-gl-tenant': 'DEMO', 'content-type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body)
	});
	const text = await res.text();
	if (!res.ok) throw new Error(`${method} ${path} as ${actor}: ${res.status} ${text}`);
	return JSON.parse(text);
}

const existing = (await call('viewer@example.com', 'GET', '/configs')).configs.find((c) => c.configId === ID);
if (existing) {
	console.log(`${ID} already exists (v${existing.latestVersion}); nothing to do.`);
	process.exit(0);
}
await call('ba@example.com', 'POST', '/configs', { configId: ID, title: 'GL-TEST-001 demo rules', effectiveFrom: '2026-01-01', content, reason: 'GL-TEST-001 demo configuration' });
await call('ba@example.com', 'POST', `/configs/${ID}/versions/1/validate`);
await call('reviewer@example.com', 'POST', `/configs/${ID}/versions/1/approve`, { reason: 'Reviewed against the GL-TEST-001 walkthrough journals' });
await call('lead@example.com', 'POST', `/configs/${ID}/versions`, { effectiveFrom: '2026-02-01', content, reason: 'February: same rules, new effective date' });
await call('lead@example.com', 'POST', `/configs/${ID}/versions/2/validate`);
console.log(`Seeded ${ID}: v1 approved (from 2026-01-01), v2 validated and awaiting a second approver.`);
