// Module entry. Importing the component registers <gl-app>; the guard makes a repeat
// import idempotent.
import App from './App.svelte';
import Workspace from './ws/Workspace.svelte';

if (typeof customElements !== 'undefined' && !customElements.get('gl-app')) {
	customElements.define('gl-app', App as unknown as CustomElementConstructor);
}
// The AI-assisted workspace (Administration → Onboarding → Stage 1), next to the classic studio.
if (typeof customElements !== 'undefined' && !customElements.get('gl-workspace')) {
	customElements.define('gl-workspace', Workspace as unknown as CustomElementConstructor);
}

export {};
