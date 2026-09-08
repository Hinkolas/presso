// The job history lives on the System page now; old links still land there.
import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = () => {
	redirect(307, '/system?tab=activity');
};
