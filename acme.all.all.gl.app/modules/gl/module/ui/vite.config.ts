import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The module's UI bundle: one custom element, <gl-app>, mounted by the app shell. The full
// configuration and simulator screens arrive with the UI stories; until then the element
// states that this module serves the /m/gl/* API.
export default defineConfig({
  plugins: [svelte({ compilerOptions: { customElement: true } })],
  build: {
    lib: { entry: 'src/entry.ts', formats: ['es'], fileName: () => 'entry.js' },
    outDir: 'dist',
    emptyOutDir: true
  }
});
