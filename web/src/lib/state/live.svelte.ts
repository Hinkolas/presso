// One EventSource on /v1/events for the whole app. Pages load their initial
// data with api.get and merge these reactive maps on top; the (app) layout
// opens the stream once and closes it when the shell unmounts. EventSource
// reconnects on its own; `connected` drives the live dot in the sidebar.
import { SvelteMap } from 'svelte/reactivity';
import { api } from '$lib/api/client';
import type { LiveEvent } from '$lib/types/events';
import type { Job, JobEvent } from '$lib/types/job';
import type { Site } from '$lib/types/site';
import type { PlatformComponent, SystemStatus } from '$lib/types/system';

const MAX_JOBS = 200;
const MAX_JOB_EVENTS = 2000;

export const live = $state({
	connected: false,
	/** Incremented on every reconnect so pages can refetch to fill gaps. */
	generation: 0,
	platform: null as PlatformComponent[] | null
});

export const sites = new SvelteMap<string, Site>();
export const deletedSites = new SvelteMap<string, true>();
export const jobs = new SvelteMap<string, Job>();
export const jobEvents = new SvelteMap<string, JobEvent[]>();

let source: EventSource | null = null;

function handle(ev: LiveEvent): void {
	switch (ev.type) {
		case 'site.status':
			sites.set(ev.payload.id, ev.payload);
			deletedSites.delete(ev.payload.id);
			break;
		case 'site.deleted':
			sites.delete(ev.payload.id);
			deletedSites.set(ev.payload.id, true);
			break;
		case 'job.updated':
			jobs.set(ev.payload.id, ev.payload);
			trimJobs();
			break;
		case 'job.event': {
			const list = jobEvents.get(ev.payload.job_id) ?? [];
			if (!list.some((e) => e.id === ev.payload.id)) {
				const next = [...list, ev.payload];
				jobEvents.set(
					ev.payload.job_id,
					next.length > MAX_JOB_EVENTS ? next.slice(-MAX_JOB_EVENTS) : next
				);
			}
			break;
		}
		case 'platform.status':
			live.platform = ev.payload;
			break;
	}
}

function trimJobs(): void {
	if (jobs.size <= MAX_JOBS) return;
	const sorted = [...jobs.values()].sort((a, b) => a.updated_at.localeCompare(b.updated_at));
	for (const j of sorted.slice(0, jobs.size - MAX_JOBS)) {
		jobs.delete(j.id);
		jobEvents.delete(j.id);
	}
}

const TYPES: LiveEvent['type'][] = [
	'site.status',
	'site.deleted',
	'job.updated',
	'job.event',
	'platform.status'
];

export function connectLive(): void {
	if (source) return;
	const es = api.stream('/v1/events');
	source = es;
	es.onopen = () => {
		live.connected = true;
		live.generation++;
	};
	es.onerror = () => {
		live.connected = false;
	};
	for (const type of TYPES) {
		es.addEventListener(type, (e) => {
			try {
				const data = JSON.parse((e as MessageEvent).data) as LiveEvent;
				handle({ ...data, type } as LiveEvent);
			} catch {
				// Malformed frame; ignore.
			}
		});
	}
}

export function disconnectLive(): void {
	source?.close();
	source = null;
	live.connected = false;
}

/** Seed job events (e.g. from GET /v1/jobs/{id}) without losing live ones. */
export function seedJobEvents(jobId: string, events: JobEvent[]): void {
	const existing = jobEvents.get(jobId) ?? [];
	const byId: Record<number, JobEvent> = {};
	for (const e of events) byId[e.id] = e;
	for (const e of existing) byId[e.id] = e;
	jobEvents.set(
		jobId,
		Object.values(byId).sort((a, b) => a.id - b.id)
	);
}

export function seedJob(job: Job): void {
	const cur = jobs.get(job.id);
	if (!cur || cur.updated_at <= job.updated_at) jobs.set(job.id, job);
}

/** Fill the live maps from the API once, so the shell (counts, health chip,
 *  breadcrumbs, palette) has data before the first event arrives. */
export async function seedLive(): Promise<void> {
	await Promise.all([
		api
			.get<{ sites: Site[] }>('/v1/sites')
			.then((r) => {
				for (const s of r.sites)
					if (!sites.has(s.id) && !deletedSites.has(s.id)) sites.set(s.id, s);
			})
			.catch(() => {}),
		api
			.get<SystemStatus>('/v1/system/status')
			.then((r) => {
				if (!live.platform) live.platform = r.components;
			})
			.catch(() => {})
	]);
}
