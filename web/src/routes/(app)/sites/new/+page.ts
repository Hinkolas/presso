// Deep links to the old page open the "New site" modal on the Sites page.
import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = () => {
	redirect(307, '/?new=1');
};
