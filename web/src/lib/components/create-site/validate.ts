// The "New site" draft and its per-step validation. Pure functions that
// mirror the engine's rules (internal/site/site.go) so most mistakes are
// caught before the POST; what only the server knows (a taken slug, a
// disabled runtime) is mapped back onto a step by `stepForServerError`.
import { pickerRows } from '$lib/state/capabilities.svelte';
import type { SiteProfile } from '$lib/types/site';
import type { RuntimeCapabilities } from '$lib/types/system';

export type Draft = {
	name: string;
	slug: string;
	/** Once the slug was typed by hand it stops following the name. */
	slugEdited: boolean;
	domain: string;
	/** WordPress branch, e.g. "6.8". */
	wp: string;
	php: string;
	/** The PHP version was chosen under Advanced rather than derived from `wp`. */
	phpOverridden: boolean;
	advancedOpen: boolean;
	profile: SiteProfile;
	pageCache: boolean;
	adminUser: string;
	adminEmail: string;
	adminPassword: string;
	showPassword: boolean;
};

export function blankDraft(): Draft {
	return {
		name: '',
		slug: '',
		slugEdited: false,
		domain: '',
		wp: '',
		php: '',
		phpOverridden: false,
		advancedOpen: false,
		profile: 'prod',
		pageCache: true,
		adminUser: 'admin',
		adminEmail: '',
		adminPassword: '',
		showPassword: false
	};
}

export type StepId = 'basics' | 'wordpress' | 'admin' | 'review';

export const STEPS: readonly { id: StepId; label: string }[] = [
	{ id: 'basics', label: 'Basics' },
	{ id: 'wordpress', label: 'WordPress' },
	{ id: 'admin', label: 'Administrator' },
	{ id: 'review', label: 'Review' }
];

export type FieldName =
	'name' | 'slug' | 'domain' | 'wp' | 'php' | 'adminUser' | 'adminEmail' | 'adminPassword';

export type FieldErrors = Partial<Record<FieldName, string>>;

/** DOM ids of the controls, for labels, error links and focus. */
export const FIELD_IDS: Record<FieldName, string> = {
	name: 'cs-name',
	slug: 'cs-slug',
	domain: 'cs-domain',
	wp: 'cs-wp',
	php: 'cs-php',
	adminUser: 'cs-admin-user',
	adminEmail: 'cs-admin-email',
	adminPassword: 'cs-admin-password'
};

export const SLUG_RE = /^[a-z0-9][a-z0-9-]{1,40}$/;
export const LOGIN_RE = /^[a-zA-Z0-9_.@-]{1,60}$/;
export const MIN_PASSWORD = 12;
const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const LABEL_RE = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

/** A bare hostname: DNS labels of letters, digits and hyphens (mirrors edge.ValidHost). */
export function isHostname(host: string): boolean {
	if (host.length === 0 || host.length > 253) return false;
	return host.split('.').every((l) => LABEL_RE.test(l));
}

export function validateBasics(d: Draft): FieldErrors {
	const errors: FieldErrors = {};
	if (!d.name.trim()) errors.name = 'Give the site a name.';
	const slug = d.slug.trim();
	if (!slug) errors.slug = 'A slug is required.';
	else if (!SLUG_RE.test(slug))
		errors.slug =
			'Lowercase letters, digits and hyphens only, 2 to 41 characters, starting with a letter or digit.';
	const domain = d.domain.trim().toLowerCase();
	if (domain) {
		if (/[\s/:]/.test(domain)) errors.domain = 'Enter a hostname only, without http:// or a path.';
		else if (!isHostname(domain)) errors.domain = 'That does not look like a hostname.';
	}
	return errors;
}

export function validateWordPress(d: Draft, caps: RuntimeCapabilities | null): FieldErrors {
	const errors: FieldErrors = {};
	if (!caps) {
		errors.wp = 'The available versions have not loaded yet.';
		return errors;
	}
	const row = pickerRows(caps).find((r) => r.branch === d.wp);
	if (!row) errors.wp = 'Choose a WordPress version.';
	else if (row.disabled) errors.wp = `${row.reason}.`;
	else if (d.php && !row.php_versions.includes(d.php))
		errors.php = `PHP ${d.php} is not available for WordPress ${row.branch} on this server.`;
	return errors;
}

export function validateAdmin(d: Draft): FieldErrors {
	const errors: FieldErrors = {};
	const user = d.adminUser.trim();
	if (!user) errors.adminUser = 'A username is required.';
	else if (!LOGIN_RE.test(user))
		errors.adminUser = 'Letters, digits and _ . @ - only, up to 60 characters.';
	const email = d.adminEmail.trim();
	if (!email) errors.adminEmail = 'An email address is required.';
	else if (!EMAIL_RE.test(email)) errors.adminEmail = 'That does not look like an email address.';
	if (d.adminPassword.length < MIN_PASSWORD)
		errors.adminPassword = `Use at least ${MIN_PASSWORD} characters.`;
	return errors;
}

export function validateStep(
	step: number,
	d: Draft,
	caps: RuntimeCapabilities | null
): FieldErrors {
	switch (STEPS[step]?.id) {
		case 'basics':
			return validateBasics(d);
		case 'wordpress':
			return validateWordPress(d, caps);
		case 'admin':
			return validateAdmin(d);
		default:
			return {};
	}
}

/** Where a POST /v1/sites error belongs: a field on a step, or a step's banner. */
export function stepForServerError(code: string): { step: number; field?: FieldName } {
	switch (code) {
		case 'slug_taken':
			return { step: 0, field: 'slug' };
		case 'domain_taken':
			return { step: 0, field: 'domain' };
		case 'wp_unsupported':
		case 'php_unsupported':
		case 'php_incompatible':
		case 'legacy_disabled':
			return { step: 1 };
		default:
			return { step: STEPS.length - 1 };
	}
}
