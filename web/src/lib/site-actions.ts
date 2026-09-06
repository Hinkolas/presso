// Lifecycle actions on a site, shared by the sites list and the site page.
// A returned job is seeded into the live map so any open view follows it.
import { api } from '$lib/api/client';
import { seedJob, sites as liveSites } from '$lib/state/live.svelte';
import type { Job } from '$lib/types/job';
import type { Site, SiteAction } from '$lib/types/site';

export type ActionResult = { job?: Job; ok?: boolean; screenshot_at?: string };

export async function runSiteAction(site: Site, action: SiteAction): Promise<ActionResult> {
	const res = await api.post<ActionResult>(`/v1/sites/${site.id}/actions/${action}`);
	if (res?.job) seedJob(res.job);
	if (action === 'screenshot' && res?.screenshot_at) {
		// The list payload carries screenshot_at; nudge the live record so every
		// preview picks up the new image without a reload.
		liveSites.set(site.id, { ...site, screenshot_at: res.screenshot_at });
	}
	return res;
}

/** Status shown for a site: container health overrides "running" when it disagrees. */
export function shownStatus(site: Site): string {
	if (site.status === 'running' && (site.health === 'unhealthy' || site.health === 'starting'))
		return site.health;
	return site.status;
}

export function screenshotUrl(site: Site): string | null {
	return site.screenshot_at
		? `/api/v1/sites/${site.id}/screenshot?v=${encodeURIComponent(site.screenshot_at)}`
		: null;
}
