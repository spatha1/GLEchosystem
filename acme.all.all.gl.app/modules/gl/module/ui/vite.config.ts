import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The module's UI bundle: one custom element, <gl-app>, that the app shell mounts. `vite build`
// writes dist/entry.js, the file the app build looks for.
//
// `vite` (dev) serves index.html, a development harness, and proxies /m/gl to the local dev
// adapter (service/cmd/devhttp, loopback only). In a real deployment the app core
// authenticates the user and sets x-mycel-user; here the proxy sets it from the harness's
// gl_dev_user cookie and drops any value the page sends. The element itself never sends an
// identity, only the tenant.
const API = process.env.GL_DEV_API ?? 'http://127.0.0.1:9080';

export default defineConfig({
	plugins: [svelte({ compilerOptions: { customElement: true } })],
	build: {
		lib: { entry: 'src/entry.ts', formats: ['es'], fileName: () => 'entry.js' },
		outDir: 'dist',
		emptyOutDir: true
	},
	server: {
		host: '127.0.0.1',
		port: 5174,
		strictPort: true,
		proxy: {
			'/m/gl': {
				target: API,
				changeOrigin: true,
				configure: (proxy) => {
					proxy.on('proxyReq', (proxyReq, req) => {
						proxyReq.removeHeader('x-mycel-user');
						const m = /(?:^|;\s*)gl_dev_user=([^;]+)/.exec(req.headers.cookie ?? '');
						if (m) proxyReq.setHeader('x-mycel-user', decodeURIComponent(m[1]));
					});
				}
			}
		}
	},
	resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
	test: {
		environment: 'jsdom',
		include: ['src/**/*.test.ts']
	}
});
