// Development harness of the workspace: picks the signed-in user (a cookie the dev proxy turns
// into x-mycel-user) and gives the element the clients the platform would list. Not part of the bundle.
import '../src/entry';

const users = [
	['admin@example.com', 'GL administrator'],
	['ba@example.com', 'Business analyst (editor)'],
	['reviewer@example.com', 'Reviewer (approver)'],
	['viewer@example.com', 'Viewer'],
	['ops@example.com', 'Operator'],
	['lead@example.com', 'Lead (editor and approver)'],
	['outsider@example.com', 'No role']
];
const clients = [
	'CLIENT_A:Client A',
	'CLIENT_B:Client B',
	// VITE_GL_DEV_CLIENTS="CODE:Label,CODE2:Label 2" adds local clients without a code change.
	...String((import.meta as { env?: Record<string, string> }).env?.VITE_GL_DEV_CLIENTS ?? '')
		.split(',')
		.map((s) => s.trim())
		.filter(Boolean)
];

function cookie(name: string): string {
	const m = new RegExp(`(?:^|;\s*)${name}=([^;]+)`).exec(document.cookie);
	return m ? decodeURIComponent(m[1]) : '';
}
const select = document.getElementById('dev-user') as HTMLSelectElement;
for (const [id, label] of users) select.add(new Option(`${label} — ${id}`, id));
if (!cookie('gl_dev_user')) document.cookie = `gl_dev_user=${encodeURIComponent(users[0][0])}; path=/; SameSite=Strict`;
select.value = cookie('gl_dev_user');
select.addEventListener('change', () => {
	document.cookie = `gl_dev_user=${encodeURIComponent(select.value)}; path=/; SameSite=Strict`;
	location.reload();
});
document.querySelector('gl-workspace')!.setAttribute('clients', clients.join(','));
