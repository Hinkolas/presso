// Shapes mirror the engine's site payloads (/v1/sites).
import type { RuntimeId } from './system';

export type SiteStatus = 'provisioning' | 'running' | 'stopped' | 'error' | 'deleting';
export type SiteHealth = 'none' | 'starting' | 'healthy' | 'unhealthy' | 'unknown';
export type TlsMode = 'auto' | 'off';
export type SiteProfile = 'prod' | 'dev';
export type FileMods = 'locked' | 'open';

export type SiteSettings = {
	php_memory_limit: string;
	upload_max_filesize: string;
	post_max_size: string;
	max_execution_time: number;
	max_input_vars: number;
	opcache_memory_mb: number;
	container_memory_mb: number;
	container_cpus: number;
	cache_ttl: number;
};

export type SiteDomain = { id: string; host: string; kind: 'primary' | 'alias' };

export type SiteMetrics1h = {
	requests: number;
	cache_hit: number;
	cache_miss: number;
	cache_bypass: number;
	cache_stale: number;
	status_5xx: number;
};

export type Site = {
	id: string;
	slug: string;
	name: string;
	status: SiteStatus;
	status_message: string;
	health: SiteHealth;
	primary_domain: string;
	url: string;
	tls_mode: TlsMode;
	php_version: string;
	runtime: RuntimeId;
	/** Pinned WordPress release, e.g. "6.0.14"; empty for sites created before it was tracked. */
	wp_version: string;
	image: string;
	profile: SiteProfile;
	file_mods: FileMods;
	page_cache: boolean;
	admin_user: string;
	admin_email: string;
	settings: SiteSettings;
	domains: SiteDomain[];
	created_at: string;
	updated_at: string;
	metrics_1h?: SiteMetrics1h;
	/** When the front-page preview was last captured; null until the first capture. */
	screenshot_at?: string | null;
};

export type CreateSiteRequest = {
	name: string;
	slug: string;
	primary_domain?: string;
	tls_mode?: TlsMode;
	/** Branch ("6.0") or pinned release ("6.0.14"). */
	wp_version: string;
	/** Defaults to the newest PHP the WordPress version supports. */
	php_version?: string;
	page_cache?: boolean;
	profile?: SiteProfile;
	admin_user: string;
	admin_email: string;
	admin_password: string;
	title?: string;
};

export type UpdateSiteRequest = {
	name?: string;
	php_version?: string;
	page_cache?: boolean;
	file_mods?: FileMods;
	tls_mode?: TlsMode;
	profile?: SiteProfile;
	settings?: Partial<SiteSettings>;
};

export type SiteAction =
	| 'start'
	| 'stop'
	| 'restart'
	| 'recreate'
	| 'purge-cache'
	| 'flush-object-cache'
	| 'retry'
	| 'screenshot';

export type WpCliResponse = {
	stdout: string;
	stderr: string;
	exit_code: number;
	truncated: boolean;
	duration_ms: number;
};

export type LoginLinkResponse = { url: string; expires_at: string };

export type SiteStats = {
	read: string;
	cpu_percent: number;
	mem_usage: number;
	mem_limit: number;
};

export type MetricsRange = '1h' | '24h' | '7d';
/** visitor: the public site; admin: wp-admin, wp-login, REST, cron, XML-RPC; all: both. */
export type MetricsScope = 'visitor' | 'admin' | 'all';

export type RequestBucket = {
	bucket: string;
	requests: number;
	cache_hit: number;
	cache_miss: number;
	cache_bypass: number;
	cache_stale: number;
	status_2xx: number;
	status_3xx: number;
	status_4xx: number;
	status_5xx: number;
	bytes: number;
	dur_p50_ms: number;
	dur_p95_ms: number;
};

export type ContainerBucket = {
	bucket: string;
	cpu_pct: number;
	mem_bytes: number;
	mem_limit: number;
};

export type MetricsResponse = {
	range: MetricsRange;
	scope: MetricsScope;
	step_seconds: number;
	requests: RequestBucket[];
	container: ContainerBucket[];
};

export type LogLine = { time: string; stream: 'stdout' | 'stderr'; text: string };
