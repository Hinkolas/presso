// Maps every status vocabulary the API uses (site status, container health,
// job status, platform component state) onto four tones. Green means good,
// brass means in motion, red means failed, neutral means at rest.
export type Tone = 'ok' | 'busy' | 'danger' | 'neutral';

const OK = new Set(['running', 'healthy', 'succeeded', 'ok', 'ready', 'connected', 'on']);
const BUSY = new Set([
	'provisioning',
	'starting',
	'queued',
	'deleting',
	'restarting',
	'created',
	'pending'
]);
const DANGER = new Set(['error', 'failed', 'unhealthy', 'dead', 'missing', 'unavailable']);

export function toneOf(status: string): Tone {
	if (OK.has(status)) return 'ok';
	if (BUSY.has(status)) return 'busy';
	if (DANGER.has(status)) return 'danger';
	return 'neutral';
}

export const toneText: Record<Tone, string> = {
	ok: 'text-ok-ink',
	busy: 'text-busy-ink',
	danger: 'text-danger-ink',
	neutral: 'text-ink-2'
};

export const toneBg: Record<Tone, string> = {
	ok: 'bg-ok',
	busy: 'bg-busy',
	danger: 'bg-danger',
	neutral: 'bg-ink-3'
};

export const toneSoft: Record<Tone, string> = {
	ok: 'bg-ok-soft text-ok-ink',
	busy: 'bg-busy-soft text-busy-ink',
	danger: 'bg-danger-soft text-danger-ink',
	neutral: 'bg-sunken text-ink-2'
};

/** Human wording for statuses the API spells in lowercase machine terms. */
export function statusLabel(status: string): string {
	switch (status) {
		case 'provisioning':
			return 'Setting up';
		case 'running':
			return 'Running';
		case 'stopped':
			return 'Stopped';
		case 'error':
			return 'Failed';
		case 'deleting':
			return 'Deleting';
		case 'queued':
			return 'Queued';
		case 'succeeded':
			return 'Succeeded';
		case 'failed':
			return 'Failed';
		case 'healthy':
			return 'Healthy';
		case 'unhealthy':
			return 'Unhealthy';
		case 'starting':
			return 'Starting';
		case 'exited':
			return 'Exited';
		case 'missing':
			return 'Missing';
		case 'ready':
			return 'Ready';
		default:
			return status ? status.charAt(0).toUpperCase() + status.slice(1) : '–';
	}
}
