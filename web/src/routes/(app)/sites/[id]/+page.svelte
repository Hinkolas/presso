<!-- Site detail: preview, identity and the main actions up top; a tab strip
     (?tab=) below. The record comes from GET /v1/sites/{id} and is kept
     fresh by live events. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api, ApiError, errorText } from '$lib/api/client';
	import { deletedSites, jobs, seedJob, sites as liveSites } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runtimeLabel } from '$lib/state/capabilities.svelte';
	import type { Job } from '$lib/types/job';
	import type { LoginLinkResponse, Site } from '$lib/types/site';
	import { formatRelative } from '$lib/format';
	import { shownStatus } from '$lib/site-actions';
	import Alert from '$lib/components/Alert.svelte';
	import BackLink from '$lib/components/BackLink.svelte';
	import Button from '$lib/components/Button.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import RuntimeBadge from '$lib/components/RuntimeBadge.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
	import Thumb from '$lib/components/Thumb.svelte';
	import Overview from './Overview.svelte';
	import Metrics from './Metrics.svelte';
	import Logs from './Logs.svelte';
	import WpCli from './WpCli.svelte';
	import Activity from './Activity.svelte';
	import Settings from './Settings.svelte';

	const id = $derived(page.params.id ?? '');

	let loaded = $state<Site | null>(null);
	let activeJobId = $state<string | null>(null);
	let error = $state('');
	let notFound = $state(false);
	let loadedFor = $state('');

	async function load(siteId: string) {
		try {
			const res = await api.get<{ site: Site; active_job: Job | null }>(`/v1/sites/${siteId}`);
			loaded = res.site;
			// Let the shell (breadcrumb, palette) know this site by name.
			if (!liveSites.has(res.site.id)) liveSites.set(res.site.id, res.site);
			if (res.active_job) {
				seedJob(res.active_job);
				activeJobId = res.active_job.id;
			}
			error = '';
			notFound = false;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'not_found') notFound = true;
			else error = errorText(err);
		}
		loadedFor = siteId;
	}

	$effect(() => {
		load(id);
	});

	const site = $derived(liveSites.get(id) ?? (loadedFor === id ? loaded : null));
	const deleted = $derived(deletedSites.has(id));

	// The job currently working on this site: the freshest unfinished one the
	// live stream knows about, else the one the detail call returned.
	const activeJob = $derived.by(() => {
		let best: Job | null = null;
		for (const j of jobs.values()) {
			if (j.site_id !== id) continue;
			if (j.status !== 'queued' && j.status !== 'running') continue;
			if (!best || j.created_at > best.created_at) best = j;
		}
		if (best) return best;
		if (activeJobId) {
			const j = jobs.get(activeJobId);
			if (j) return j;
		}
		return null;
	});

	type Tab = 'overview' | 'metrics' | 'logs' | 'wp' | 'activity' | 'settings';
	const tabs: { id: Tab; label: string; admin?: boolean }[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'metrics', label: 'Metrics' },
		{ id: 'logs', label: 'Logs' },
		{ id: 'wp', label: 'WP-CLI', admin: true },
		{ id: 'activity', label: 'Activity' },
		{ id: 'settings', label: 'Settings' }
	];
	const visibleTabs = $derived(tabs.filter((t) => !t.admin || isAdmin()));
	const tab = $derived.by<Tab>(() => {
		const q = page.url.searchParams.get('tab') as Tab | null;
		return q && visibleTabs.some((t) => t.id === q) ? q : 'overview';
	});

	function setTab(t: Tab) {
		const search = t === 'overview' ? '' : `?tab=${t}`;
		goto(resolve(`/sites/${id}${search}`), { replaceState: true, noScroll: true, keepFocus: true });
	}

	function onJob(job: Job) {
		seedJob(job);
		activeJobId = job.id;
	}

	let adminBusy = $state(false);
	let adminError = $state('');
	async function openAdmin() {
		if (!site) return;
		adminBusy = true;
		adminError = '';
		try {
			const res = await api.post<LoginLinkResponse>(`/v1/sites/${site.id}/login-link`);
			window.open(res.url, '_blank', 'noopener');
		} catch (err) {
			adminError = errorText(err);
		} finally {
			adminBusy = false;
		}
	}

	const shown = $derived(site ? shownStatus(site) : '');
</script>

<svelte:head>
	<title>{site?.name ?? 'Site'} · presso</title>
</svelte:head>

<BackLink href={resolve('/')} label="Sites" class="mb-4" />

{#if deleted}
	<Alert tone="info">
		This site has been deleted.
		<a href={resolve('/')} class="underline underline-offset-2">Back to sites</a>
	</Alert>
{:else if notFound}
	<Alert>
		There is no site with this id.
		<a href={resolve('/')} class="underline underline-offset-2">Back to sites</a>
	</Alert>
{:else if error && !site}
	<Alert>{error}</Alert>
	<div class="mt-4"><Button onclick={() => load(id)}>Try again</Button></div>
{:else if !site}
	<div class="flex gap-8" aria-busy="true">
		<Skeleton plate class="aspect-[16/10] w-72" />
		<div class="flex flex-1 flex-col gap-3 pt-2">
			<Skeleton class="h-10 w-1/3" />
			<Skeleton class="h-5 w-1/4" />
		</div>
	</div>
{:else}
	<header class="mb-8 flex flex-col gap-6 sm:flex-row sm:flex-wrap sm:items-start sm:gap-8">
		<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
		<a
			href={site.url}
			target="_blank"
			rel="noreferrer noopener"
			class="group relative block w-full shrink-0 overflow-hidden frame transition-shadow hover:shadow-plate-hover sm:w-72"
			aria-label="Visit {site.name}"
		>
			<Thumb {site} size="sm" class="aspect-[16/10]" />
			<span
				class="absolute right-3 bottom-3 grid size-8 place-items-center rounded-full bg-surface/90 text-ink opacity-0 shadow-sm transition-opacity group-hover:opacity-100"
			>
				<Icon name="external" size={14} />
			</span>
		</a>
		<!-- eslint-enable svelte/no-navigation-without-resolve -->

		<div class="min-w-0 flex-1 pt-1">
			<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
				<h1 class="display text-3xl text-ink sm:text-4xl">{site.name}</h1>
				<StatusPill status={shown} soft />
				<RuntimeBadge runtime={site.runtime} size="md" />
			</div>
			<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
			<a
				href={site.url}
				target="_blank"
				rel="noreferrer noopener"
				class="mt-2 inline-flex items-center gap-1.5 text-md text-ink-2 hover:text-ink"
			>
				{site.primary_domain}
				<Icon name="external" size={15} class="text-ink-3" />
			</a>
			<!-- eslint-enable svelte/no-navigation-without-resolve -->
			{#if site.status_message}
				<p
					class="mt-3 max-w-2xl text-sm {site.status === 'error'
						? 'text-danger-ink'
						: 'text-ink-2'}"
				>
					{site.status_message}
				</p>
			{/if}
			<p class="mt-3 text-sm text-ink-3">
				{site.wp_version ? `WordPress ${site.wp_version} on ` : ''}PHP {site.php_version}
				({runtimeLabel(site.runtime)}), {site.profile === 'dev' ? 'development' : 'production'} profile,
				{#if site.screenshot_at}
					preview from {formatRelative(site.screenshot_at)}
				{:else}
					no preview yet
				{/if}
			</p>
		</div>

		<div class="flex flex-col items-start gap-2 sm:items-end">
			{#if isAdmin()}
				<Button
					variant="primary"
					icon="key"
					onclick={openAdmin}
					busy={adminBusy}
					disabled={site.status !== 'running'}
				>
					Open wp-admin
				</Button>
			{/if}
			{#if adminError}
				<span class="text-xs text-danger-ink">{adminError}</span>
			{/if}
		</div>
	</header>

	<nav
		class="mb-8 flex [scrollbar-width:none] gap-2 overflow-x-auto border-b border-rule"
		aria-label="Site sections"
	>
		{#each visibleTabs as t (t.id)}
			<button
				type="button"
				onclick={() => setTab(t.id)}
				class="group relative -mb-px h-12 px-1 text-base font-medium transition-colors {tab === t.id
					? 'text-ink'
					: 'text-ink-2 hover:text-ink'}"
				aria-current={tab === t.id ? 'page' : undefined}
			>
				<span
					class="inline-flex h-9 items-center rounded-chip px-3 transition-colors group-hover:bg-sunken {tab ===
					t.id
						? 'group-hover:bg-transparent'
						: ''}"
				>
					{t.label}
				</span>
				{#if tab === t.id}
					<span class="absolute inset-x-4 bottom-0 h-0.5 rounded-t bg-brand"></span>
				{/if}
			</button>
		{/each}
	</nav>

	{#if tab === 'overview'}
		<Overview {site} {activeJob} {onJob} />
	{:else if tab === 'metrics'}
		<Metrics siteId={site.id} />
	{:else if tab === 'logs'}
		<Logs siteId={site.id} />
	{:else if tab === 'wp' && isAdmin()}
		<WpCli siteId={site.id} running={site.status === 'running'} />
	{:else if tab === 'activity'}
		<Activity siteId={site.id} />
	{:else if tab === 'settings'}
		<Settings {site} {onJob} />
	{/if}
{/if}
