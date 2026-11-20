// Development harness: picks the signed-in user (a cookie the dev proxy turns into
// x-mycel-user) and the tenant attribute of <gl-app>. Not part of the module bundle.
import '../src/entry';

const users = [
	['ba@example.com', 'Business analyst (editor)'],
	['reviewer@example.com', 'Reviewer (approver)'],
	['lead@example.com', 'Lead (editor and approver)'],
	['viewer@example.com', 'Viewer'],
	['outsider@example.com', 'No role in DEMO']
];

function cookie(name: string): string {
	const m = new RegExp(`(?:^|;\\s*)${name}=([^;]+)`).exec(document.cookie);
	return m ? decodeURIComponent(m[1]) : '';
}

const select = document.getElementById('dev-user') as HTMLSelectElement;
const tenant = document.getElementById('dev-tenant') as HTMLInputElement;
const app = document.querySelector('gl-app') as HTMLElement;

for (const [id, label] of users) {
	select.add(new Option(`${label} — ${id}`, id));
}
if (!cookie('gl_dev_user')) {
	document.cookie = `gl_dev_user=${encodeURIComponent(users[0][0])}; path=/; SameSite=Strict`;
}
select.value = cookie('gl_dev_user');
select.addEventListener('change', () => {
	document.cookie = `gl_dev_user=${encodeURIComponent(select.value)}; path=/; SameSite=Strict`;
	location.reload();
});
tenant.addEventListener('change', () => app.setAttribute('tenant', tenant.value.trim().toUpperCase()));
