<!-- Settings tab: PATCH the editable site fields. The save bar at the bottom
     says up front whether the pending changes apply on save or recreate the
     container (everything except the name and the cache TTL does); a
     returned job means a recreate is under way. -->
<script lang="ts">
	import { untrack } from 'svelte';
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
	import Input from '$lib/components/Input.svelte';
	import NumberInput from '$lib/components/NumberInput.svelte';
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
	let saved = $state(false);

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

	const pending = $derived(diff());
	const dirty = $derived(Object.keys(pending).length > 0);
	// Mirrors internal/site/ops.go: the name and the cache TTL apply in place,
	// everything else means a new container.
	const recreates = $derived.by(() => {
		const { name: _n, settings, ...rest } = pending;
		void _n;
		if (Object.keys(rest).length) return true;
		if (!settings) return false;
		const { cache_ttl: _t, ...others } = settings;
		void _t;
		return Object.keys(others).length > 0;
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		error = '';
		saved = false;
		try {
			const res = await api.patch<{ site: Site; job: Job | null }>(`/v1/sites/${site.id}`, diff());
			// A recreate shows in the page's job toast; in-place saves get a line here.
			if (res.job) onJob(res.job);
			else saved = true;
			seen = res.site.updated_at;
			form = snapshot(res.site);
		} catch (err) {
			error = errorText(err);
		} finally {
			saving = false;
		}
	}

	function discard() {
		form = snapshot(site);
		error = '';
	}
</script>

<form onsubmit={save} class="flex flex-col gap-5">
	<Panel title="General">
		<div class="grid gap-x-6 gap-y-5 sm:grid-cols-2">
			<Field label="Name" for="s-name">
				<Input id="s-name" bind:value={form.name} required autocomplete="off" />
			</Field>
			<Field label="WordPress" hint="Chosen when the site was created.">
				<div class="flex h-11 items-center gap-2.5 text-sm text-ink">
					<span class="num">
						{site.wp_version ? `WordPress ${site.wp_version}` : 'Not detected yet'}
					</span>
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
				hint={form.profile === 'dev'
					? 'Debug logging on, caches short. For building, not for visitors.'
					: 'Tuned for visitors: opcache warm, errors logged not shown.'}
			>
				<div>
					<Segmented
						bind:value={form.profile}
						label="Profile"
						options={[
							{ value: 'prod', label: 'Production' },
							{ value: 'dev', label: 'Development' }
						]}
					/>
				</div>
			</Field>
			<Field
				label="Plugin and theme installs"
				hint={form.file_mods === 'locked'
					? 'wp-admin cannot install or update plugins, themes or core.'
					: 'wp-admin can install and update plugins and themes.'}
			>
				<div>
					<Segmented
						bind:value={form.file_mods}
						label="Plugin and theme installs"
						options={[
							{ value: 'locked', label: 'Locked' },
							{ value: 'open', label: 'Allowed' }
						]}
					/>
				</div>
			</Field>
		</div>
	</Panel>

	<Panel title="PHP">
		<div class="grid gap-x-6 gap-y-5 sm:grid-cols-3">
			<Field label="Memory limit" for="s-mem" hint="Per request, e.g. 256M.">
				<Input id="s-mem" mono bind:value={form.settings.php_memory_limit} spellcheck="false" />
			</Field>
			<Field label="Upload max filesize" for="s-upload" hint="e.g. 64M.">
				<Input
					id="s-upload"
					mono
					bind:value={form.settings.upload_max_filesize}
					spellcheck="false"
				/>
			</Field>
			<Field label="Post max size" for="s-post" hint="At least the upload size.">
				<Input id="s-post" mono bind:value={form.settings.post_max_size} spellcheck="false" />
			</Field>
			<Field label="Max execution time" for="s-exec" hint="Per request.">
				<NumberInput
					id="s-exec"
					min={0}
					step={5}
					suffix="s"
					bind:value={form.settings.max_execution_time}
				/>
			</Field>
			<Field label="Max input vars" for="s-vars" hint="Raise for very large menus or forms.">
				<NumberInput id="s-vars" min={0} step={1000} bind:value={form.settings.max_input_vars} />
			</Field>
			<Field label="OPcache memory" for="s-opcache" hint="Compiled PHP kept in memory.">
				<NumberInput
					id="s-opcache"
					min={0}
					step={16}
					suffix="MB"
					bind:value={form.settings.opcache_memory_mb}
				/>
			</Field>
		</div>
	</Panel>

	<div class="grid gap-5 lg:grid-cols-2">
		<Panel title="Container">
			<div class="grid gap-x-6 gap-y-5 sm:grid-cols-2">
				<Field label="Memory limit" for="s-cmem" hint="The container is killed above it.">
					<NumberInput
						id="s-cmem"
						min={0}
						step={128}
						suffix="MB"
						bind:value={form.settings.container_memory_mb}
					/>
				</Field>
				<Field label="CPUs" for="s-cpus" hint="Fractions allowed, e.g. 0.5.">
					<NumberInput
						id="s-cpus"
						min={0}
						step={0.25}
						suffix="CPUs"
						bind:value={form.settings.container_cpus}
					/>
				</Field>
			</div>
		</Panel>

		<Panel title="Edge cache">
			<div class="grid gap-x-6 gap-y-5 sm:grid-cols-2">
				<div class="pt-1 sm:col-span-2">
					<Toggle
						bind:checked={form.page_cache}
						label="Page cache at the edge"
						hint="Anonymous page views are served from the edge and purged when content changes."
					/>
				</div>
				<Field
					label="Time to live"
					for="s-ttl"
					hint={form.page_cache
						? 'How long a page stays cached at the edge.'
						: 'Applies once the page cache is on.'}
				>
					<NumberInput
						id="s-ttl"
						min={0}
						step={60}
						suffix="s"
						disabled={!form.page_cache}
						bind:value={form.settings.cache_ttl}
					/>
				</Field>
			</div>
		</Panel>
	</div>

	{#if error}
		<Alert>{error}</Alert>
	{/if}
	{#if saved}
		<Alert tone="success">Settings saved.</Alert>
	{/if}

	<div
		class="sticky bottom-4 flex flex-wrap items-center justify-end gap-x-4 gap-y-3 rounded-plate border border-rule/70 bg-surface/85 px-5 py-3.5 shadow-plate backdrop-blur-md transition-[opacity,transform] duration-200 {dirty ||
		saving
			? 'opacity-100'
			: 'pointer-events-none translate-y-2 opacity-0'}"
		aria-hidden={!dirty && !saving}
	>
		<p class="mr-auto text-sm text-ink-2">
			{#if recreates}
				Saving recreates the container. The site is unavailable for a few seconds while it comes
				back.
			{:else}
				These changes apply as soon as you save.
			{/if}
		</p>
		<Button onclick={discard} disabled={!dirty || saving}>Discard changes</Button>
		<Button type="submit" variant="primary" busy={saving} disabled={!dirty}>Save changes</Button>
	</div>
</form>
