<!-- Site detail: header, tab strip (?tab=) and the tab panels. The site
     record comes from GET /v1/sites/{id} and is kept fresh by live events. -->
<script lang="ts">
	import { replaceState } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api, ApiError, errorText } from '$lib/api/client';
	import { deletedSites, jobs, seedJob, sites as liveSites } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import type { Job } from '$lib/types/job';
	import type { Site } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
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
		// Shallow routing: swap the query without re-running loads.
		const search = t === 'overview' ? '' : `?tab=${t}`;
		replaceState(resolve(`/sites/${id}${search}`), {});
	}

	function onJob(job: Job) {
		seedJob(job);
		activeJobId = job.id;
	}
</script>

<svelte:head>
	<title>{site?.name ?? 'Site'} · presso</title>
</svelte:head>

<div class="mb-4 flex items-center gap-3">
	<a href={resolve('/')} class="text-sm text-gray-500 hover:text-gray-900">← Sites</a>
</div>

{#if deleted}
	<Alert tone="info">
		This site has been deleted. <a href={resolve('/')} class="underline">Back to sites</a>
	</Alert>
{:else if notFound}
	<Alert>Site not found. <a href={resolve('/')} class="underline">Back to sites</a></Alert>
{:else if error && !site}
	<Alert>{error}</Alert>
	<div class="mt-3"><Button onclick={() => load(id)}>Retry</Button></div>
{:else if !site}
	<p class="text-sm text-gray-500">Loading…</p>
{:else}
	<header class="mb-5">
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="text-xl font-semibold">{site.name}</h1>
			<StatusPill status={site.status} />
			{#if site.status === 'running' && site.health !== 'none'}
				<StatusPill status={site.health} dot={false} />
			{/if}
			<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
			<a
				href={site.url}
				target="_blank"
				rel="noreferrer noopener"
				class="text-sm text-gray-600 hover:underline">{site.primary_domain} ↗</a
			>
			<!-- eslint-enable svelte/no-navigation-without-resolve -->
		</div>
		{#if site.status_message}
			<p class="mt-1 text-sm {site.status === 'error' ? 'text-red-700' : 'text-gray-500'}">
				{site.status_message}
			</p>
		{/if}
	</header>

	<nav class="mb-5 flex gap-1 border-b border-gray-200" aria-label="Site sections">
		{#each visibleTabs as t (t.id)}
			<button
				type="button"
				onclick={() => setTab(t.id)}
				class="-mb-px border-b-2 px-3 py-2 text-sm font-medium {tab === t.id
					? 'border-gray-900 text-gray-900'
					: 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-800'}"
				aria-current={tab === t.id ? 'page' : undefined}
			>
				{t.label}
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
