// Seeds the local dev adapter (service/cmd/devhttp, loopback only) with the GL-TEST-001
// reference configuration for Client A, through the API and its maker-checker flow:
//   GL_TEST_001 v1 (from 2026-01-01): created and validated by ba@, approved by reviewer@.
// Client B gets nothing, to show that one client's configuration is invisible to another.
// Requires GL_DEV_SEED_ROLES on devhttp for CLIENT_A and CLIENT_B: ba@ CONFIG_EDITOR,
// reviewer@ CONFIG_APPROVER, lead@ CONFIG_EDITOR|CONFIG_APPROVER, viewer@ VIEWER.
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const API = (process.env.GL_DEV_API ?? 'http://127.0.0.1:9080') + '/m/gl';
const fixture = fileURLToPath(new URL('../../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json', import.meta.url));
const content = JSON.parse(await readFile(fixture, 'utf8'));
const TENANT = 'CLIENT_A';
const ID = 'GL_TEST_001';

async function call(actor, method, path, body) {
	const res = await fetch(API + path, {
		method,
		headers: { 'x-mycel-user': actor, 'x-gl-tenant': TENANT, 'content-type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body)
	});
	const text = await res.text();
	if (!res.ok) throw new Error(`${method} ${path} as ${actor}: ${res.status} ${text}`);
	return JSON.parse(text);
}

const existing = (await call('viewer@example.com', 'GET', '/configs')).configs.find((c) => c.configId === ID);
if (existing) {
	console.log(`${ID} already exists for ${TENANT} (v${existing.latestVersion}); nothing to do.`);
	process.exit(0);
}
await call('ba@example.com', 'POST', '/configs', {
	configId: ID,
	title: 'GL-TEST-001 reference configuration (policy P1)',
	effectiveFrom: '2026-01-01',
	content,
	reason: 'GL-TEST-001 reference configuration'
});
await call('ba@example.com', 'POST', `/configs/${ID}/versions/1/validate`);
await call('reviewer@example.com', 'POST', `/configs/${ID}/versions/1/approve`, { reason: 'Reviewed against the GL-TEST-001 walkthrough journals' });
console.log(`Seeded ${TENANT} ${ID}: v1 approved (from 2026-01-01).`);
