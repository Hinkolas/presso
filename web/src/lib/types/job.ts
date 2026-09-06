// Shapes mirror the engine's job payloads (/v1/jobs).

export type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed';

export type Job = {
	id: string;
	kind: string;
	site_id: string | null;
	status: JobStatus;
	args: Record<string, unknown>;
	error: string;
	created_by: string;
	started_at: string | null;
	finished_at: string | null;
	created_at: string;
	updated_at: string;
};

export type JobEvent = {
	id: number;
	job_id: string;
	ts: string;
	level: 'step' | 'info' | 'warn' | 'error';
	step: string;
	message: string;
};

export type JobsResponse = { jobs: Job[] };
export type JobDetailResponse = { job: Job; events: JobEvent[] };
