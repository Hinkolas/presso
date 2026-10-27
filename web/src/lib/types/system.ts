// Shapes mirror the engine's system payloads (/v1/system/*).

export type DockerInfo = {
	ok: boolean;
	version: string;
	api_version: string;
	os: string;
	arch: string;
	error?: string;
};

export type ComponentName = 'network' | 'mariadb' | 'valkey' | 'edge' | 'browser';

export type PlatformComponent = {
	name: ComponentName;
	state: string;
	health: string;
	image: string;
	error?: string;
};

export type SystemStatus = {
	version: string;
	docker: DockerInfo;
	components: PlatformComponent[];
	edge: { last_applied_at: string | null; last_error: string; sites: number };
	sites: Record<string, number>;
	disk: {
		data_dir_bytes: number;
		images_bytes: number;
		containers_bytes: number;
		volumes_bytes: number;
	};
	uptime_seconds: number;
	/** Self-update state; absent when the engine runs without the updater. */
	update?: UpdateStatus;
};

/** Self-update state (/v1/system/status `update`, system.update events). */
export type UpdateStatus = {
	/** Running version, e.g. "v0.1.0" or "v0.0.0-dev". */
	current: string;
	release_build: boolean;
	/** Whether this process can replace its own binary; `unsupported_reason` says why not. */
	supported: boolean;
	unsupported_reason?: string;
	/** The admin's settings: the release channel and automatic updates. */
	channel: UpdateChannel;
	auto_update: boolean;
	/** Whether the background check runs (UPDATE_CHECK); automatic updates need it. */
	checks: boolean;
	/** Newest published release on the channel; "" before the first successful check. */
	latest: string;
	/** `latest` is an alpha, beta or rc release. */
	prerelease: boolean;
	available: boolean;
	/** The running version is newer than `latest` (back on stable after beta): no downgrade. */
	ahead: boolean;
	url: string;
	published_at: string | null;
	checked_at: string | null;
	error: string;
	error_kind?: UpdateErrorKind;
	/** Queued or running engine.update / engine.rollout job. */
	job_id: string | null;
	job_kind?: string;
};

export type UpdateChannel = 'stable' | 'beta';

export type UpdateErrorKind =
	'offline' | 'not_found' | 'rate_limited' | 'unavailable' | 'invalid' | 'no_release';

export type SystemMeta = { version: string };

// Runtime capabilities (/v1/system/runtime): what this server can run.
// `frankenphp` is the default runtime (Caddy + PHP, PHP 8.2 and newer);
// `legacy` is nginx + PHP-FPM for WordPress releases that need PHP 7.4 to 8.1.
export type RuntimeId = 'frankenphp' | 'legacy';

export type RuntimeDescriptor = {
	id: RuntimeId;
	label: string;
	description: string;
	/** Newest first. Kept even when the runtime is disabled on this server. */
	php_versions: string[];
	enabled: boolean;
};

export type WordPressStatus = 'latest' | 'outdated';

export type WordPressBranch = {
	/** Release branch, e.g. "6.8". */
	branch: string;
	/** Pinned release the image ships, e.g. "6.8.8". */
	version: string;
	status: WordPressStatus;
	/** PHP versions this server offers for the branch, newest first. */
	php_versions: string[];
	default_php: string;
	/** Runtime of `default_php`. */
	runtime: RuntimeId;
};

export type RuntimeCapabilities = {
	runtimes: RuntimeDescriptor[];
	/** Newest first. Branches with no offered PHP are omitted. */
	wordpress: WordPressBranch[];
	default_wp: string;
	legacy_enabled: boolean;
};
