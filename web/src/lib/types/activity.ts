// Shapes mirror the engine's activity payloads (/v1/sites/{id}/activity).

export type Activity = {
	id: string;
	ts: string;
	user_id: string | null;
	site_id: string | null;
	kind: string;
	message: string;
	meta: Record<string, unknown>;
};

export type ActivityResponse = { activity: Activity[] };
