// Auth guard for the whole app area: asks the API who the cookie belongs to.
// A 401 sends the visitor to sign in; an unreachable API renders an error
// state instead of bouncing between pages.
import { redirect } from '@sveltejs/kit';
import type { LayoutLoad } from './$types';
import { setUser } from '$lib/state/session.svelte';
import type { SessionResponse } from '$lib/types/auth';

export const load: LayoutLoad = async ({ url }) => {
	let res: Response;
	try {
		res = await fetch('/api/v1/auth/session', {
			credentials: 'same-origin',
			headers: { 'X-Requested-With': 'presso' }
		});
	} catch {
		setUser(null);
		return { user: null, apiDown: true };
	}
	if (res.status === 401) {
		setUser(null);
		redirect(302, '/auth/login?next=' + encodeURIComponent(url.pathname + url.search));
	}
	if (!res.ok) {
		setUser(null);
		return { user: null, apiDown: true };
	}
	const data = (await res.json()) as SessionResponse;
	setUser(data.user);
	return { user: data.user, apiDown: false };
};
