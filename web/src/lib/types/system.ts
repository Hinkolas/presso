// Shapes mirror the engine's system payloads (/v1/system/*).

export type DockerInfo = {
	ok: boolean;
	version: string;
	api_version: string;
	os: string;
	arch: string;
	error?: string;
};

export type ComponentName = 'network' | 'mariadb' | 'redis' | 'edge';

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
};

export type SystemMeta = { version: string };

export type RuntimeInfo = { php_versions: string[]; default_php: string };
