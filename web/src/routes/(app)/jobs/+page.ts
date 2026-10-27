// The job history lives on Settings → System; old links still land there.
import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = () => {
	redirect(307, '/settings/system?tab=activity');
};
