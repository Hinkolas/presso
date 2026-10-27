import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { readFileSync } from 'node:fs';

// The UI is a static SPA: adapter-static emits web/build with an index.html
// fallback, and the Go binary embeds and serves it next to /api. In dev the
// Vite server proxies /api to a locally running engine so the session cookie
// it sets is first-party on http://localhost:5173.
export default defineConfig({
	plugins: [
		{
			name: 'verify-bundled-notices',
			apply: 'build',
			generateBundle() {
				const covered = new Set(
					(
						JSON.parse(readFileSync('static/third-party-manifest.json', 'utf8')) as {
							name: string;
						}[]
					).map((p) => p.name)
				);
				for (const id of this.getModuleIds()) {
					const match = id.match(/node_modules\/((?:@[^/]+\/)?[^/]+)/);
					if (match && !covered.has(match[1])) {
						this.error(
							`Missing shipped dependency notice for ${match[1]}; update scripts/licenses.mjs`
						);
					}
				}
			}
		},
		tailwindcss(),
		sveltekit({
			alias: { $lib: 'src/lib' },
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ fallback: 'index.html', precompress: false, strict: false })
		})
	],
	server: {
		proxy: {
			'/api': { target: 'http://localhost:7080', changeOrigin: false }
		}
	}
});
