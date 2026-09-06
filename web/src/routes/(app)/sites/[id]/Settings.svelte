<!-- Settings tab: PATCH the editable site fields; a returned job means the
     container is being recreated to apply them. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import type { Job } from '$lib/types/job';
	import type { Site, SiteSettings, UpdateSiteRequest } from '$lib/types/site';
	import type { RuntimeInfo } from '$lib/types/system';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Field from '$lib/components/Field.svelte';
	import Toggle from '$lib/components/Toggle.svelte';

	let { site, onJob }: { site: Site; onJob: (j: Job) => void } = $props();

	let runtime = $state<RuntimeInfo | null>(null);
	$effect(() => {
		api
			.get<RuntimeInfo>('/v1/system/runtime')
			.then((r) => (runtime = r))
			.catch(() => (runtime = { php_versions: [site.php_version], default_php: site.php_version }));
	});

	// Editable copy of the site; reset whenever the live record changes id or
	// updated_at (i.e. after our own save or someone else's).
	let form = $state(untrack(() => snapshot(site)));
	let seen = $state(untrack(() => site.updated_at));
	$effect(() => {
		if (site.updated_at !== seen) {
			seen = site.updated_at;
			form = snapshot(site);
		}
	});

	function snapshot(s: Site) {
		return {
			name: s.name,
			php_version: s.php_version,
			page_cache: s.page_cache,
			file_mods: s.file_mods,
			tls_mode: s.tls_mode,
			profile: s.profile,
			settings: { ...s.settings } as SiteSettings
		};
	}

	const isLocal = $derived(site.primary_domain.endsWith('.localhost'));

	let saving = $state(false);
	let error = $state('');
	let saved = $state<Job | null | 'ok'>(null);

	function diff(): UpdateSiteRequest {
		const body: UpdateSiteRequest = {};
		if (form.name !== site.name) body.name = form.name;
		if (form.php_version !== site.php_version) body.php_version = form.php_version;
		if (form.page_cache !== site.page_cache) body.page_cache = form.page_cache;
		if (form.file_mods !== site.file_mods) body.file_mods = form.file_mods;
		if (form.tls_mode !== site.tls_mode) body.tls_mode = form.tls_mode;
		if (form.profile !== site.profile) body.profile = form.profile;
		const settings: Partial<SiteSettings> = {};
		for (const k of Object.keys(form.settings) as (keyof SiteSettings)[]) {
			if (form.settings[k] !== site.settings[k]) {
				(settings as Record<string, unknown>)[k] = form.settings[k];
			}
		}
		if (Object.keys(settings).length) body.settings = settings;
		return body;
	}

	const dirty = $derived(Object.keys(diff()).length > 0);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		error = '';
		saved = null;
		try {
			const res = await api.patch<{ site: Site; job: Job | null }>(`/v1/sites/${site.id}`, diff());
			if (res.job) {
				onJob(res.job);
				saved = res.job;
			} else {
				saved = 'ok';
			}
			seen = res.site.updated_at;
			form = snapshot(res.site);
		} catch (err) {
			error = errorText(err);
		} finally {
			saving = false;
		}
	}

	const num = (v: string) => (v === '' ? 0 : Number(v));
</script>

<form onsubmit={save} class="flex max-w-3xl flex-col gap-4">
	<Card title="General">
		<div class="grid gap-4 sm:grid-cols-2">
			<Field label="Name" for="s-name">
				<input id="s-name" class="input" bind:value={form.name} required />
			</Field>
			<Field label="PHP version" for="s-php">
				<select id="s-php" class="input" bind:value={form.php_version} disabled={!runtime}>
					{#each runtime?.php_versions ?? [site.php_version] as v (v)}
						<option value={v}>{v}</option>
					{/each}
				</select>
			</Field>
			<Field label="Profile" for="s-profile">
				<select id="s-profile" class="input" bind:value={form.profile}>
					<option value="prod">prod</option>
					<option value="dev">dev</option>
				</select>
			</Field>
			<Field
				label="File modifications"
				for="s-mods"
				hint="Locked blocks plugin/theme installs and core updates from wp-admin."
			>
				<select id="s-mods" class="input" bind:value={form.file_mods}>
					<option value="locked">locked</option>
					<option value="open">open</option>
				</select>
			</Field>
			<Field
				label="TLS"
				for="s-tls"
				hint={isLocal ? 'Not available for .localhost domains.' : undefined}
			>
				<select id="s-tls" class="input" bind:value={form.tls_mode} disabled={isLocal}>
					<option value="auto">auto</option>
					<option value="off">off</option>
				</select>
			</Field>
			<div class="flex items-end pb-1.5">
				<Toggle bind:checked={form.page_cache} label="Page cache" />
			</div>
		</div>
	</Card>

	<Card title="PHP">
		<div class="grid gap-4 sm:grid-cols-3">
			<Field label="memory_limit" for="s-mem">
				<input id="s-mem" class="input font-mono" bind:value={form.settings.php_memory_limit} />
			</Field>
			<Field label="upload_max_filesize" for="s-upload">
				<input
					id="s-upload"
					class="input font-mono"
					bind:value={form.settings.upload_max_filesize}
				/>
			</Field>
			<Field label="post_max_size" for="s-post">
				<input id="s-post" class="input font-mono" bind:value={form.settings.post_max_size} />
			</Field>
			<Field label="max_execution_time (s)" for="s-exec">
				<input
					id="s-exec"
					type="number"
					min="0"
					class="input font-mono"
					value={form.settings.max_execution_time}
					oninput={(e) => (form.settings.max_execution_time = num(e.currentTarget.value))}
				/>
			</Field>
			<Field label="max_input_vars" for="s-vars">
				<input
					id="s-vars"
					type="number"
					min="0"
					class="input font-mono"
					value={form.settings.max_input_vars}
					oninput={(e) => (form.settings.max_input_vars = num(e.currentTarget.value))}
				/>
			</Field>
			<Field label="OPcache memory (MB)" for="s-opcache">
				<input
					id="s-opcache"
					type="number"
					min="0"
					class="input font-mono"
					value={form.settings.opcache_memory_mb}
					oninput={(e) => (form.settings.opcache_memory_mb = num(e.currentTarget.value))}
				/>
			</Field>
		</div>
	</Card>

	<Card title="Container and cache">
		<div class="grid gap-4 sm:grid-cols-3">
			<Field label="Memory limit (MB)" for="s-cmem">
				<input
					id="s-cmem"
					type="number"
					min="0"
					class="input font-mono"
					value={form.settings.container_memory_mb}
					oninput={(e) => (form.settings.container_memory_mb = num(e.currentTarget.value))}
				/>
			</Field>
			<Field label="CPUs" for="s-cpus">
				<input
					id="s-cpus"
					type="number"
					min="0"
					step="0.25"
					class="input font-mono"
					value={form.settings.container_cpus}
					oninput={(e) => (form.settings.container_cpus = num(e.currentTarget.value))}
				/>
			</Field>
			<Field label="Page cache TTL (s)" for="s-ttl">
				<input
					id="s-ttl"
					type="number"
					min="0"
					class="input font-mono"
					value={form.settings.cache_ttl}
					oninput={(e) => (form.settings.cache_ttl = num(e.currentTarget.value))}
				/>
			</Field>
		</div>
	</Card>

	{#if error}
		<Alert>{error}</Alert>
	{/if}
	{#if saved === 'ok'}
		<Alert tone="success">Settings saved.</Alert>
	{:else if saved}
		<Alert tone="info">
			Applying: recreating container.
			<a href={resolve(`/jobs/${saved.id}`)} class="underline">Follow the job</a>
		</Alert>
	{/if}

	<div class="flex justify-end gap-2">
		<Button onclick={() => (form = snapshot(site))} disabled={!dirty || saving}>Reset</Button>
		<Button type="submit" variant="primary" busy={saving} disabled={!dirty}>Save</Button>
	</div>
</form>
