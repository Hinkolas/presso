import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { apiFetch, clientMeta, setSessionCookie } from '$lib/server/api';
import { safeNext } from '$lib/server/safe-next';

// `next` carries the page a guard bounced from through the form.
export const load: PageServerLoad = async ({ locals, url }) => {
	const next = safeNext(url.searchParams.get('next'));
	if (locals.user) redirect(302, next);
	return { next };
};

type ApiErrorBody = { error?: { code?: string; message?: string } };

function errorMessage(status: number, body: ApiErrorBody | null): string {
	switch (body?.error?.code) {
		case 'invalid_credentials':
			return 'Wrong email or password.';
		case 'rate_limited':
			return 'Too many attempts. Wait a moment and try again.';
		default:
			return status >= 500 || status === 0
				? 'The server is unreachable. Try again in a moment.'
				: (body?.error?.message ?? 'Something went wrong.');
	}
}

// The bearer token only ever touches the BFF: the action exchanges the
// credentials for a session and stores the token in the httpOnly cookie.
export const actions: Actions = {
	default: async ({ request, cookies, fetch, getClientAddress }) => {
		const form = await request.formData();
		const email = String(form.get('email') ?? '').trim();
		const password = String(form.get('password') ?? '');
		const next = safeNext(String(form.get('next') ?? ''));
		if (!email || !password) {
			return fail(400, { message: 'Email and password are required.', email });
		}

		let res: Response;
		try {
			res = await apiFetch(fetch, null, '/v1/auth/login', {
				method: 'POST',
				body: JSON.stringify({ email, password }),
				headers: clientMeta(request, getClientAddress)
			});
		} catch {
			return fail(503, { message: errorMessage(0, null), email });
		}
		const body = await res.json().catch(() => null);
		if (!res.ok) {
			return fail(res.status, { message: errorMessage(res.status, body), email });
		}

		setSessionCookie(cookies, body.session.token, body.session.expires_at);
		redirect(303, next);
	}
};
