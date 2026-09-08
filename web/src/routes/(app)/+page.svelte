<!-- Sites: every site on this host, as a grid of plates or a list of rows.
     The initial GET is merged with live site.status events; the list view
     also fetches an hour of request buckets per site for p95. Under 44rem of
     content width the list gives way to the grid card, which stacks into one
     column and is the right shape for a phone. -->
<script lang="ts">
	import { SvelteMap } from 'svelte/reactivity';
	import { afterNavigate, replaceState } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { api, errorText } from '$lib/api/client';
	import { openCreateSite } from '$lib/state/create-site.svelte';
	import { deletedSites, live, sites as liveSites } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import type { MetricsResponse, Site, SiteStatus } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Empty from '$lib/components/Empty.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Plate from '$lib/components/Plate.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Select from '$lib/components/Select.svelte';
	import SiteRow from '$lib/components/SiteRow.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';

	type View = 'grid' | 'list';
	const VIEW_KEY = 'presso.sites.view';
	let view = $state<View>('grid');
	// The list needs room for its columns; narrower than this the grid card
	// takes over and the toggle goes away. Same number as the row's comment.
	const LIST_MIN = 44 * 16;
	let width = $state(Infinity);
	const narrow = $derived(width < LIST_MIN);
	const shownView = $derived<View>(narrow ? 'grid' : view);
	$effect(() => {
		// ?view= wins for this visit; otherwise the remembered choice.
		const q = page.url.searchParams.get('view');
		if (q === 'grid' || q === 'list') {
			view = q;
			return;
		}
		try {
			const v = localStorage.getItem(VIEW_KEY);
			if (v === 'grid' || v === 'list') view = v;
		} catch {
			// Storage unavailable; the default stands.
		}
	});
	function setView(v: View) {
		view = v;
		try {
			localStorage.setItem(VIEW_KEY, v);
		} catch {
			// Not remembered this time.
		}
	}

	// /?new (also where /sites/new lands) opens the wizard, then the URL is
	// cleaned so a reload or a back-step does not reopen it.
	afterNavigate(() => {
		if (!page.url.searchParams.has('new')) return;
		openCreateSite();
		replaceState(resolve('/'), {});
	});

	type Filter = 'all' | SiteStatus;
	let filter = $state<Filter>('all');

	let loaded = $state<Site[] | null>(null);
	let error = $state('');

	async function load() {
		try {
			const res = await api.get<{ sites: Site[] }>('/v1/sites');
			loaded = res.sites;
			error = '';
		} catch (err) {
			error = errorText(err);
			loaded ??= [];
		}
	}
	$effect(() => {
		void live.generation;
		load();
	});

	// Live map wins over the snapshot; sites created after load appear too.
	const all = $derived.by(() => {
		const byId: Record<string, Site> = {};
		for (const s of loaded ?? []) byId[s.id] = s;
		for (const s of liveSites.values()) {
			// Live events carry no metrics; keep the snapshot's summary.
			byId[s.id] = { ...s, metrics_1h: s.metrics_1h ?? byId[s.id]?.metrics_1h };
		}
		for (const id of deletedSites.keys()) delete byId[id];
		return Object.values(byId).sort((a, b) => a.name.localeCompare(b.name));
	});

	const shown = $derived(filter === 'all' ? all : all.filter((s) => s.status === filter));

	const counts = $derived.by(() => {
		const c: Partial<Record<SiteStatus, number>> = {};
		for (const s of all) c[s.status] = (c[s.status] ?? 0) + 1;
		return c;
	});

	const words = ['no', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine'];
	const count = (n: number) => (n < words.length ? words[n] : String(n));
	const summary = $derived.by(() => {
		const n = all.length;
		if (n === 0) return 'No sites on this host yet.';
		const parts = [
			`${count(n).replace(/^./, (c) => c.toUpperCase())} ${n === 1 ? 'site' : 'sites'}`
		];
		if (counts.running) parts.push(`${count(counts.running)} running`);
		if (counts.stopped) parts.push(`${count(counts.stopped)} stopped`);
		if (counts.provisioning) parts.push(`${count(counts.provisioning)} being set up`);
		if (counts.error) parts.push(`${count(counts.error)} failing`);
		if (counts.deleting) parts.push(`${count(counts.deleting)} being deleted`);
		return parts.join(', ') + '.';
	});

	// The list view shows p95, which needs the hour's buckets.
	const hours = new SvelteMap<string, MetricsResponse>();
	async function loadHour(id: string) {
		try {
			hours.set(id, await api.get<MetricsResponse>(`/v1/sites/${id}/metrics?range=1h`));
		} catch {
			// The column shows a dash.
		}
	}
	$effect(() => {
		if (shownView !== 'list') return;
		const ids = all.filter((s) => s.status === 'running').map((s) => s.id);
		for (const id of ids) loadHour(id);
		const t = setInterval(() => ids.forEach(loadHour), 60_000);
		return () => clearInterval(t);
	});

	const filterOptions: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All statuses' },
		{ value: 'running', label: 'Running' },
		{ value: 'provisioning', label: 'Setting up' },
		{ value: 'stopped', label: 'Stopped' },
		{ value: 'error', label: 'Failed' }
	];
</script>

<svelte:head>
	<title>Sites · presso</title>
</svelte:head>

<PageHeader title="Sites" subtitle={loaded === null ? 'Loading your sites.' : summary}>
	{#snippet actions()}
		{#if all.length > 0}
			<Select bind:value={filter} options={filterOptions} label="Filter by status" />
			{#if !narrow}
				<Segmented
					value={view}
					options={[
						{ value: 'grid', label: 'Grid', icon: 'grid' },
						{ value: 'list', label: 'List', icon: 'list' }
					]}
					iconOnly
					label="View"
					onchange={setView}
				/>
			{/if}
		{/if}
		{#if isAdmin()}
			<Button variant="primary" onclick={openCreateSite} icon="plus">New site</Button>
		{/if}
	{/snippet}
</PageHeader>

<!-- Measured so the list can hand over to the grid card when the column is
     narrow; also the container the rows' column queries resolve against. -->
<div class="@container" bind:clientWidth={width}>
	{#if error}
		<Alert class="mb-6">{error}</Alert>
	{/if}

	{#if loaded === null}
		<div class="grid gap-7 lg:grid-cols-2" aria-busy="true">
			{#each [0, 1] as i (i)}
				<div class="overflow-hidden frame">
					<div class="aspect-[16/10] animate-pulse bg-sunken"></div>
					<div class="flex flex-col gap-3 border-t border-rule p-6">
						<Skeleton class="h-6 w-1/2" />
						<Skeleton class="h-4 w-1/3" />
					</div>
				</div>
			{/each}
		</div>
	{:else if all.length === 0}
		<Empty
			title="Create your first site"
			text="A new WordPress site takes about a minute: the runtime container, its database and cache, and a route at the edge."
		>
			{#if isAdmin()}
				<Button variant="primary" onclick={openCreateSite} icon="plus">New site</Button>
			{/if}
		</Empty>
	{:else if shown.length === 0}
		<Empty title="No site matches" text="Nothing has this status right now.">
			<Button onclick={() => (filter = 'all')}>Show all statuses</Button>
		</Empty>
	{:else if shownView === 'grid'}
		<div class="grid gap-7 lg:grid-cols-2">
			{#each shown as s (s.id)}
				<Plate site={s} />
			{/each}
		</div>
	{:else}
		<div class="flex flex-col gap-3">
			{#each shown as s (s.id)}
				<SiteRow site={s} hour={hours.get(s.id)} />
			{/each}
		</div>
	{/if}
</div>
