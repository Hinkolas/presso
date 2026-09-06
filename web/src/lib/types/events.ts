// Shapes of the /v1/events SSE stream. Each message carries `event: <type>`
// and a JSON envelope whose payload depends on the type.

import type { Job, JobEvent } from './job';
import type { Site } from './site';
import type { PlatformComponent } from './system';

export type LiveEvent =
	| { type: 'site.status'; time: string; payload: Site }
	| { type: 'site.deleted'; time: string; payload: { id: string } }
	| { type: 'job.updated'; time: string; payload: Job }
	| { type: 'job.event'; time: string; payload: JobEvent }
	| { type: 'platform.status'; time: string; payload: PlatformComponent[] };

export type LiveEventType = LiveEvent['type'];
