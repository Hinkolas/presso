// What this server can run: WordPress branches, PHP versions and the two
// runtimes, from GET /v1/system/runtime. Fetched once and shared by the
// create-site wizard, the site pages and Settings; in-flight requests are
// deduplicated and the cache is refreshed after a live-stream reconnect
// (`live.generation` moves), when the server may have restarted with a new
// policy.
import { api } from '$lib/api/client';
import type { RuntimeCapabilities, RuntimeId, WordPressBranch } from '$lib/types/system';
import { live } from './live.svelte';

export const capabilities = $state<{ current: RuntimeCapabilities | null }>({ current: null });

let inflight: Promise<RuntimeCapabilities> | null = null;
let fetchedGeneration = -1;

/** Resolve the capabilities, from cache when fresh. Rejects with an ApiError. */
export function loadCapabilities(force = false): Promise<RuntimeCapabilities> {
	if (inflight) return inflight;
	const cached = capabilities.current;
	if (cached && !force && fetchedGeneration === live.generation) return Promise.resolve(cached);
	const generation = live.generation;
	inflight = api
		.get<RuntimeCapabilities>('/v1/system/runtime')
		.then((r) => {
			capabilities.current = r;
			fetchedGeneration = generation;
			return r;
		})
		.finally(() => {
			inflight = null;
		});
	return inflight;
}

export const LEGACY_DISABLED_REASON = 'Legacy runtime disabled on this server';

/**
 * A branch as the version picker shows it. The server lists every published
 * branch; one whose only built PHP versions belong to the disabled legacy
 * runtime arrives with an empty `php_versions` and is shown greyed out with
 * the reason.
 */
export type PickerRow = WordPressBranch & { disabled: boolean; reason?: string };

export function pickerRows(caps: RuntimeCapabilities | null): PickerRow[] {
	if (!caps) return [];
	return caps.wordpress.map((b) =>
		b.php_versions.length > 0
			? { ...b, disabled: false }
			: { ...b, disabled: true, reason: LEGACY_DISABLED_REASON }
	);
}

/** "6.8.8" → "6.8"; "7.1" → "7.1"; "" → "". */
export function branchOf(version: string): string {
	const parts = version.split('.');
	return parts.length >= 2 ? `${parts[0]}.${parts[1]}` : version;
}

export function findBranch(
	caps: RuntimeCapabilities | null,
	branch: string
): WordPressBranch | undefined {
	return caps?.wordpress.find((b) => b.branch === branch);
}

/** The runtime that ships a PHP version, whether or not it is enabled here. */
export function runtimeOfPhp(caps: RuntimeCapabilities | null, php: string): RuntimeId | null {
	return caps?.runtimes.find((r) => r.php_versions.includes(php))?.id ?? null;
}

export function runtimeEnabled(caps: RuntimeCapabilities | null, id: RuntimeId | null): boolean {
	if (!id) return true;
	return caps?.runtimes.find((r) => r.id === id)?.enabled ?? true;
}

/** The one home for how the runtimes are named in the UI. */
export function runtimeLabel(id: RuntimeId | string | undefined): string {
	switch (id) {
		case 'frankenphp':
			return 'FrankenPHP';
		case 'legacy':
			return 'Legacy · nginx + PHP-FPM';
		default:
			return id || '–';
	}
}

/** Secondary line for a PHP option: which runtime it lands on, or why it cannot be chosen. */
export function phpHint(caps: RuntimeCapabilities | null, php: string): string {
	const rt = runtimeOfPhp(caps, php);
	if (!rt) return '';
	if (!runtimeEnabled(caps, rt)) return LEGACY_DISABLED_REASON;
	return runtimeLabel(rt);
}

/** Newest first, numerically ("8.10" would sort above "8.9"). */
export function sortPhp(versions: Iterable<string>): string[] {
	const key = (v: string) => v.split('.').map((n) => Number(n) || 0);
	return Array.from(versions)
		.filter((v, i, all) => all.indexOf(v) === i)
		.sort((a, b) => {
			const [am, an] = key(a);
			const [bm, bn] = key(b);
			return bm - am || bn - an;
		});
}

/**
 * PHP choices for a branch: exactly what the server offers, which is what CI
 * built for it. The server never lists a version without an image, so there
 * is nothing to grey out here.
 */
export function phpOptionsFor(
	caps: RuntimeCapabilities | null,
	branch: WordPressBranch | undefined
): { value: string; label: string; hint: string; disabled: boolean }[] {
	if (!caps || !branch) return [];
	return sortPhp(branch.php_versions).map((v) => ({
		value: v,
		label: `PHP ${v}`,
		hint: phpHint(caps, v),
		disabled: false
	}));
}
