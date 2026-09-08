// Turns a job and its event log into a step-by-step picture: which planned
// steps are done, which one is running, what is still to come. The plan
// arrives with the job payload; a step the handler reports outside the plan
// is slotted in where it happened.
import type { Job, JobEvent } from '$lib/types/job';

export type StepState = 'done' | 'active' | 'failed' | 'todo' | 'skipped';

export type ProgressStep = {
	name: string;
	label: string;
	state: StepState;
	/** The handler's own wording for the step, once it has fired. */
	message: string;
	startedAt: string | null;
	/** When the next step began or the job finished; null while active. */
	endedAt: string | null;
	lines: JobEvent[];
};

export type Progress = {
	steps: ProgressStep[];
	/** Lines logged before any step fired. */
	preamble: JobEvent[];
	/** Index of the step that fired last, -1 before the first. */
	position: number;
	total: number;
	/** 0..1 for a bar: a live job sits halfway into its current step. */
	fraction: number;
	/** The step under way, or the one that failed. */
	current: ProgressStep | null;
	live: boolean;
};

export const isLive = (job: Job): boolean => job.status === 'queued' || job.status === 'running';

export function jobProgress(job: Job, events: JobEvent[]): Progress {
	const live = isLive(job);
	const blank = (name: string, label: string): ProgressStep => ({
		name,
		label,
		state: 'todo',
		message: '',
		startedAt: null,
		endedAt: null,
		lines: []
	});
	const steps = (job.plan ?? []).map((p) => blank(p.name, p.label));
	const preamble: JobEvent[] = [];
	let last: ProgressStep | null = null;
	for (const ev of events) {
		if (ev.level !== 'step') {
			(last ? last.lines : preamble).push(ev);
			continue;
		}
		let s = steps.find((x) => x.name === ev.step && x.startedAt === null);
		if (!s) {
			s = blank(ev.step, ev.message);
			steps.splice(last ? steps.indexOf(last) + 1 : 0, 0, s);
		}
		s.startedAt = ev.ts;
		s.message = ev.message;
		if (last) last.endedAt = ev.ts;
		last = s;
	}
	if (last && job.finished_at) last.endedAt = job.finished_at;

	const position = last ? steps.indexOf(last) : -1;
	for (const [i, s] of steps.entries()) {
		if (s.startedAt) {
			s.state = s !== last ? 'done' : live ? 'active' : job.status === 'failed' ? 'failed' : 'done';
		} else if (i < position || job.status === 'succeeded') {
			s.state = 'skipped';
		}
	}

	const total = steps.length;
	let fraction: number;
	if (job.status === 'succeeded') fraction = 1;
	else if (total === 0) fraction = 0;
	else fraction = Math.min(1, (position + 0.5) / total);

	return {
		steps,
		preamble,
		position,
		total,
		fraction,
		current: last && (live || job.status === 'failed') ? last : null,
		live
	};
}

/** "site.provision" → "provision". */
export const jobVerb = (kind: string): string => kind.split('.').pop() ?? kind;

const WORDING: Record<string, { doing: string; done: (site: string) => string }> = {
	provision: { doing: 'Setting up', done: (s) => `${s} is running` },
	delete: { doing: 'Deleting', done: (s) => `${s} was deleted` },
	start: { doing: 'Starting', done: (s) => `${s} is running` },
	stop: { doing: 'Stopping', done: (s) => `${s} is stopped` },
	restart: { doing: 'Restarting', done: (s) => `${s} is back` },
	recreate: { doing: 'Recreating', done: (s) => `${s} is back` }
};

/** One line naming the operation for a person: "Setting up Test", "Test is running". */
export function jobTitle(job: Job, siteName: string): string {
	const verb = jobVerb(job.kind);
	const w = WORDING[verb] ?? {
		doing: verb.charAt(0).toUpperCase() + verb.slice(1),
		done: (s: string) => `${s}: done`
	};
	if (job.status === 'succeeded') return w.done(siteName);
	if (job.status === 'failed') return `${w.doing} ${siteName} failed`;
	return `${w.doing} ${siteName}`;
}

/** The short present participle alone: "Setting up". */
export function jobDoing(job: Job): string {
	const verb = jobVerb(job.kind);
	return WORDING[verb]?.doing ?? verb.charAt(0).toUpperCase() + verb.slice(1);
}
