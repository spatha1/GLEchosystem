// Module entry. Importing the component registers <gl-app>; the guard makes a repeat
// import idempotent.
import App from './App.svelte';

if (typeof customElements !== 'undefined' && !customElements.get('gl-app')) {
	customElements.define('gl-app', App as unknown as CustomElementConstructor);
}

export {};
