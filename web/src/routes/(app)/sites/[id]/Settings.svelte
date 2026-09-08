<!-- Settings tab: PATCH the editable site fields; a returned job means the
     container is being recreated to apply them. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import {
		branchOf,
		capabilities,
		findBranch,
		loadCapabilities,
		phpOptionsFor,
		runtimeLabel,
		runtimeOfPhp
	} from '$lib/state/capabilities.svelte';
	import type { Job } from '$lib/types/job';
	import type {
		FileMods,
		Site,
		SiteProfile,
		SiteSettings,
		UpdateSiteRequest
	} from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import RuntimeBadge from '$lib/components/RuntimeBadge.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Select from '$lib/components/Select.svelte';
	import Toggle from '$lib/components/Toggle.svelte';

	let { site, onJob }: { site: Site; onJob: (j: Job) => void } = $props();

	// PHP choices are bounded by the site's WordPress version: what the matrix
	// allows for its branch, intersected with what this server offers.
	$effect(() => {
		loadCapabilities().catch(() => {});
	});
	const caps = $derived(capabilities.current);
	const branch = $derived(findBranch(caps, branchOf(site.wp_version ?? '')));
	const phpOptions = $derived.by(() => {
		const opts = phpOptionsFor(caps, branch);
		if (!opts.some((o) => o.value === site.php_version)) {
			opts.unshift({
				value: site.php_version,
				label: `PHP ${site.php_version}`,
				hint: runtimeLabel(site.runtime),
				disabled: false
			});
		}
		return opts;
	});
	const phpLocked = $derived(!caps || !branch);

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

	const phpRuntime = $derived(runtimeOfPhp(caps, form.php_version) ?? site.runtime);
	const runtimeMoves = $derived(
		form.php_version !== site.php_version && phpRuntime !== site.runtime
	);

	function snapshot(s: Site) {
		return {
			name: s.name,
			php_version: s.php_version,
			page_cache: s.page_cache,
			file_mods: s.file_mods as FileMods,
			tls_mode: s.tls_mode,
			profile: s.profile as SiteProfile,
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

<form onsubmit={save} class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
	<div class="flex flex-col gap-5">
		<Panel title="General">
			<div class="grid gap-5 sm:grid-cols-2">
				<Field label="Name" for="s-name">
					<input id="s-name" class="input" bind:value={form.name} required />
				</Field>
				<Field label="WordPress" for="s-wp" hint="Chosen when the site was created.">
					<div id="s-wp" class="flex input items-center gap-2 bg-sunken text-ink-2">
						<span class="num"
							>{site.wp_version ? `WordPress ${site.wp_version}` : 'Not detected yet'}</span
						>
						<RuntimeBadge runtime={site.runtime} />
					</div>
				</Field>
				<Field
					label="PHP version"
					for="s-php"
					hint={!caps
						? undefined
						: !branch
							? 'Available once the WordPress version has been detected.'
							: `WordPress ${branch.branch} runs on PHP ${branch.php_versions.join(', ')}.`}
				>
					<Select
						id="s-php"
						block
						bind:value={form.php_version}
						disabled={phpLocked}
						options={phpOptions}
					/>
				</Field>
				{#if runtimeMoves}
					<div class="sm:col-span-2">
						<Alert tone="warn">
							PHP {form.php_version} moves the site from {runtimeLabel(site.runtime)} to
							{runtimeLabel(phpRuntime)}. The container is recreated.
						</Alert>
					</div>
				{/if}
				<Field
					label="Profile"
					for="s-profile"
					hint={form.profile === 'dev'
						? 'Debug logging on, caches short. For building, not for visitors.'
						: 'Tuned for visitors: opcache warm, errors logged not shown.'}
				>
					<div>
						<Segmented
							bind:value={form.profile}
							options={[
								{ value: 'prod', label: 'Production' },
								{ value: 'dev', label: 'Development' }
							]}
						/>
					</div>
				</Field>
				<Field
					label="Plugin and theme installs"
					for="s-mods"
					hint={form.file_mods === 'locked'
						? 'wp-admin cannot install or update plugins, themes or core.'
						: 'wp-admin can install and update plugins and themes.'}
				>
					<div>
						<Segmented
							bind:value={form.file_mods}
							options={[
								{ value: 'locked', label: 'Locked' },
								{ value: 'open', label: 'Allowed' }
							]}
						/>
					</div>
				</Field>
				<Field
					label="TLS"
					for="s-tls"
					hint={isLocal
						? 'Not available for .localhost domains.'
						: 'Automatic obtains and renews a certificate for the domain.'}
				>
					<Select
						id="s-tls"
						block
						bind:value={form.tls_mode}
						disabled={isLocal}
						options={[
							{ value: 'auto', label: 'Automatic certificate' },
							{ value: 'off', label: 'Off' }
						]}
					/>
				</Field>
				<div class="flex items-end pb-1">
					<Toggle
						bind:checked={form.page_cache}
						label="Page cache at the edge"
						hint="Anonymous page views are served from the edge."
					/>
				</div>
			</div>
		</Panel>

		<Panel title="PHP">
			<div class="grid gap-5 sm:grid-cols-3">
				<Field label="Memory limit" for="s-mem" hint="e.g. 256M">
					<input id="s-mem" class="input font-mono" bind:value={form.settings.php_memory_limit} />
				</Field>
				<Field label="Upload max filesize" for="s-upload" hint="e.g. 64M">
					<input
						id="s-upload"
						class="input font-mono"
						bind:value={form.settings.upload_max_filesize}
					/>
				</Field>
				<Field label="Post max size" for="s-post" hint="At least the upload size.">
					<input id="s-post" class="input font-mono" bind:value={form.settings.post_max_size} />
				</Field>
				<Field label="Max execution time" for="s-exec" hint="Seconds per request.">
					<input
						id="s-exec"
						type="number"
						min="0"
						class="input font-mono"
						value={form.settings.max_execution_time}
						oninput={(e) => (form.settings.max_execution_time = num(e.currentTarget.value))}
					/>
				</Field>
				<Field label="Max input vars" for="s-vars" hint="Raise for very large menus or forms.">
					<input
						id="s-vars"
						type="number"
						min="0"
						class="input font-mono"
						value={form.settings.max_input_vars}
						oninput={(e) => (form.settings.max_input_vars = num(e.currentTarget.value))}
					/>
				</Field>
				<Field label="OPcache memory" for="s-opcache" hint="MB of compiled PHP kept in memory.">
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
		</Panel>

		<Panel title="Container and cache">
			<div class="grid gap-5 sm:grid-cols-3">
				<Field label="Memory limit" for="s-cmem" hint="MB. The container is killed above it.">
					<input
						id="s-cmem"
						type="number"
						min="0"
						class="input font-mono"
						value={form.settings.container_memory_mb}
						oninput={(e) => (form.settings.container_memory_mb = num(e.currentTarget.value))}
					/>
				</Field>
				<Field label="CPUs" for="s-cpus" hint="Fractions allowed, e.g. 0.5.">
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
				<Field label="Page cache TTL" for="s-ttl" hint="Seconds a page stays cached at the edge.">
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
		</Panel>

		{#if error}
			<Alert>{error}</Alert>
		{/if}
		{#if saved === 'ok'}
			<Alert tone="success">Settings saved.</Alert>
		{:else if saved}
			<Alert tone="info">
				Recreating the container with the new settings.
				<a href={resolve(`/jobs/${saved.id}`)} class="underline underline-offset-2"
					>Follow the job</a
				>
			</Alert>
		{/if}

		<div class="flex items-center justify-end gap-3">
			{#if dirty}
				<span class="text-xs text-busy">Unsaved changes</span>
			{/if}
			<Button onclick={() => (form = snapshot(site))} disabled={!dirty || saving}>
				Discard changes
			</Button>
			<Button type="submit" variant="primary" busy={saving} disabled={!dirty}>Save changes</Button>
		</div>
	</div>

	<aside class="lg:sticky lg:top-0 lg:self-start">
		<Panel title="Applying changes">
			<div class="flex flex-col gap-3 text-sm leading-5 text-ink-2">
				<p>The name and the page cache switch apply as soon as you save.</p>
				<p>
					PHP version, profile, install policy, container limits and PHP settings recreate the
					container. The site is unavailable for a few seconds while it comes back.
				</p>
				{#if saved && saved !== 'ok'}
					<p class="border-t border-rule pt-3">
						A recreate is in progress.
						<a href={resolve(`/jobs/${saved.id}`)} class="text-ink underline underline-offset-2">
							Follow the job
						</a>
					</p>
				{/if}
			</div>
		</Panel>
	</aside>
</form>
