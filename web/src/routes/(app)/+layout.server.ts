// Shell data for the whole authenticated area. Runs once per hard load and
// again on invalidateAll(); tab navigation does not re-run it.

import { redirect } from '@sveltejs/kit';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ locals, url }) => {
	if (!locals.user) {
		redirect(302, '/auth/login?next=' + encodeURIComponent(url.pathname + url.search));
	}
	return { user: locals.user };
};
