// Notices for the current browser runtime and fonts. Build-only tools are not
// distributed as browser code. Update this list when adding runtime imports;
// it is not a complete container/binary dependency inventory.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = new URL('../', import.meta.url);
const packages = [
	['@fontsource-variable/instrument-sans', 'LICENSE'],
	['@fontsource/ibm-plex-mono', 'LICENSE'],
	['@fontsource/instrument-serif', 'LICENSE'],
	['@sveltejs/kit', 'LICENSE'],
	['svelte', 'LICENSE.md'],
	['uplot', 'LICENSE'],
	['devalue', 'LICENSE'],
	['esm-env', 'LICENSE'],
	['clsx', 'license']
];

const notices = ['Third-party browser code and font notices\n'];
for (const [name, license] of packages) {
	const dir = new URL(`node_modules/${name}/`, root);
	const { version } = JSON.parse(readFileSync(new URL('package.json', dir), 'utf8'));
	const text = readFileSync(new URL(license, dir), 'utf8');
	if (!text.trim()) throw new Error(`Missing license text for ${name}`);
	notices.push(`\n${'='.repeat(72)}\n${name} ${version}\n${'='.repeat(72)}\n\n${text}`);
}
const output = new URL('static/third-party-licenses.txt', root);
writeFileSync(output, notices.join('\n'));
console.log(`Wrote ${packages.length} dependency notices to ${fileURLToPath(output)}`);
