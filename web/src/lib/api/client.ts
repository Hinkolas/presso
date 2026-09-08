// Thin client for the engine API at the same origin (/api/v1/…). The session
// lives in an httpOnly cookie set by the API; every request carries the
// X-Requested-With header the API's CSRF guard demands on cookie-authenticated
// mutations. Components never build URLs themselves.

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

const BASE = '/api';

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
	const headers: Record<string, string> = { 'X-Requested-With': 'presso' };
	if (body !== undefined) headers['content-type'] = 'application/json';
	let res: Response;
	try {
		res = await fetch(`${BASE}${path}`, {
			method,
			headers,
			credentials: 'same-origin',
			body: body !== undefined ? JSON.stringify(body) : undefined
		});
	} catch {
		throw new ApiError(0, 'network', 'The server is unreachable.');
	}
	if (res.status === 204) return undefined as T;
	const data = await res.json().catch(() => null);
	if (!res.ok) {
		const detail = data?.error;
		// A 401 usually means the session died mid-use: a full navigation lands
		// on the login page. invalid_credentials is a user error (wrong
		// password in a settings flow), not a dead session.
		if (res.status === 401 && detail?.code !== 'invalid_credentials') {
			const next = window.location.pathname + window.location.search;
			window.location.href = '/auth/login?next=' + encodeURIComponent(next);
		}
		throw new ApiError(res.status, detail?.code ?? 'internal', detail?.message ?? res.statusText);
	}
	return data as T;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
	patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
	put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
	del: <T = void>(path: string, body?: unknown) => request<T>('DELETE', path, body),
	/** Open an SSE stream; the session cookie travels automatically. */
	stream: (path: string) => new EventSource(`${BASE}${path}`)
};

/** Human copy for the error codes the UI is expected to handle. */
export function errorText(err: unknown, fallback = 'Something went wrong.'): string {
	if (!(err instanceof ApiError)) return fallback;
	switch (err.code) {
		case 'network':
			return 'The server is unreachable.';
		case 'bad_request':
			return err.message || 'The request was invalid.';
		case 'slug_taken':
			return 'That slug is already in use.';
		case 'domain_taken':
			return 'That domain is already attached to another site.';
		case 'wp_unsupported':
			return 'This WordPress version is not available on this server.';
		case 'php_unsupported':
			return 'This PHP version is not available on this server.';
		case 'php_incompatible':
			return 'This WordPress version does not run on the chosen PHP version.';
		case 'legacy_disabled':
			return 'The legacy runtime (nginx + PHP-FPM) is disabled on this server.';
		case 'job_running':
			return 'Another operation is in progress on this site.';
		case 'site_not_ready':
			return 'The site is not running.';
		case 'docker_unavailable':
			return 'Docker is unavailable.';
		case 'engine_updating':
			return 'presso is updating itself. Site operations resume after it restarts.';
		case 'update_unavailable':
			return err.message || 'Self-update is not available on this host.';
		case 'update_current':
			return 'presso is already on the latest release.';
		case 'no_release':
			return 'No release has been published yet.';
		case 'panel_via_edge':
			return err.message || 'The panel is served through the edge.';
		case 'not_found':
			return 'Not found.';
		case 'forbidden':
			return 'You are not allowed to do that.';
		case 'invalid_credentials':
			return 'Wrong email or password.';
		case 'rate_limited':
			return 'Too many attempts. Wait a moment and try again.';
		default:
			return err.message || fallback;
	}
}
