<!-- App shell: a sidebar with Sites up top and, pinned to the bottom, System
     (platform health sits on its row) and the account card; a topbar for the
     breadcrumb and search; the page below. One event stream is opened here
     and shared by every page. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api } from '$lib/api/client';
	import {
		connectLive,
		deletedSites,
		disconnectLive,
		jobs,
		live,
		seedLive,
		sites
	} from '$lib/state/live.svelte';
	import { session, setUser } from '$lib/state/session.svelte';
	import Button from '$lib/components/Button.svelte';
	import CommandPalette from '$lib/components/CommandPalette.svelte';
	import CreateSiteModal from '$lib/components/create-site/CreateSiteModal.svelte';
	import Icon, { type IconName } from '$lib/components/Icon.svelte';
	import Menu, { type MenuItem } from '$lib/components/Menu.svelte';
	import Wordmark from '$lib/components/Wordmark.svelte';
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: Snippet } = $props();

	const user = $derived(session.user ?? data.user);

	type NavItem = { href: string; label: string; icon: IconName; match: (p: string) => boolean };
	const primary: NavItem[] = [
		{
			href: resolve('/'),
			label: 'Sites',
			icon: 'sites',
			match: (p) => p === '/' || p.startsWith('/sites')
		}
	];
	// System also owns the job history (its Activity tab and /jobs/{id}).
	const system: NavItem = {
		href: resolve('/system'),
		label: 'System',
		icon: 'system',
		match: (p) => p.startsWith('/system') || p.startsWith('/jobs')
	};

	const siteCount = $derived.by(() => {
		let n = 0;
		for (const id of sites.keys()) if (!deletedSites.has(id)) n++;
		return n || null;
	});
	const runningJobs = $derived.by(() => {
		let n = 0;
		for (const j of jobs.values()) if (j.status === 'queued' || j.status === 'running') n++;
		return n;
	});

	// Platform health for the dot on the System row: every component reported
	// ok. A dropped event stream is surfaced here too, since the data on screen
	// may be stale until it reconnects. Running jobs show there as well, since
	// the job history is a tab of System.
	const platformState = $derived.by(() => {
		if (!live.connected && live.generation > 0) return 'offline';
		const comps = live.platform;
		if (!comps || comps.length === 0) return null;
		const bad = comps.some(
			(c) => c.error || c.state === 'error' || c.state === 'missing' || c.health === 'unhealthy'
		);
		return bad ? 'error' : 'ok';
	});
	const platformTitle = $derived(
		platformState === 'error'
			? 'A component needs attention'
			: platformState === 'offline'
				? 'Reconnecting live updates'
				: 'All components healthy'
	);

	// Topbar context: the section, then the page within it.
	const crumbs = $derived.by(() => {
		const p = page.url.pathname;
		if (p.startsWith('/sites/')) return ['Sites', sites.get(page.params.id ?? '')?.name ?? 'Site'];
		if (p.startsWith('/jobs/')) return ['System', 'Activity', 'Job'];
		if (p.startsWith('/system')) {
			return page.url.searchParams.get('tab') === 'activity' ? ['System', 'Activity'] : ['System'];
		}
		if (p.startsWith('/account')) return ['Account security'];
		return ['Sites'];
	});

	let paletteOpen = $state(false);
	// Below lg the sidebar is a drawer opened from the topbar.
	let navOpen = $state(false);
	let mainEl: HTMLElement | undefined = $state();

	// The content pane is the scroller, so a new page starts at the top
	// (tab switches change only the query and keep their position). A
	// navigation also closes the drawer.
	$effect(() => {
		void page.url.pathname;
		mainEl?.scrollTo({ top: 0 });
		navOpen = false;
	});

	$effect(() => {
		if (data.apiDown) return;
		connectLive();
		seedLive();
		return () => disconnectLive();
	});

	function onKey(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			paletteOpen = !paletteOpen;
		} else if (e.key === 'Escape' && navOpen) {
			navOpen = false;
		}
	}

	async function signOut() {
		try {
			await api.post('/v1/auth/logout');
		} catch {
			// The API's session sweeper collects leftovers.
		}
		setUser(null);
		await goto(resolve('/auth/login'));
	}

	const initials = $derived(
		(user?.name || user?.email || '?')
			.split(/[\s@.]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((s) => s[0])
			.join('')
			.toUpperCase()
	);
	const firstName = $derived((user?.name || user?.email || '').split(/[\s@]+/)[0] || 'Account');

	const accountItems: MenuItem[] = [
		{ label: 'Account security', icon: 'user', href: resolve('/account') },
		{ label: 'Sign out', icon: 'logout', onclick: signOut }
	];
</script>

<svelte:window onkeydown={onKey} />

{#snippet navLink(item: NavItem)}
	{@const active = item.match(page.url.pathname)}
	<!-- eslint-disable svelte/no-navigation-without-resolve -- resolved above -->
	<a
		href={item.href}
		class="flex h-11 items-center gap-3 rounded-control-sm px-3 text-base font-medium transition-colors {active
			? 'bg-brand-soft text-ink'
			: 'text-ink-2 hover:bg-sunken hover:text-ink'}"
		aria-current={active ? 'page' : undefined}
	>
		<Icon name={item.icon} size={20} class={active ? 'text-brand' : 'text-ink-3'} />
		<span class="flex-1">{item.label}</span>
		{#if item.label === 'Sites' && siteCount}
			<span class="text-sm text-ink-3 num">{siteCount}</span>
		{:else if item.label === 'System' && platformState && platformState !== 'ok'}
			<span
				class="size-2 rounded-full {platformState === 'error' ? 'bg-danger' : 'breathe bg-busy'}"
				title={platformTitle}
			></span>
		{:else if item.label === 'System' && runningJobs}
			<span class="size-2 breathe rounded-full bg-busy" title="{runningJobs} running"></span>
		{:else if item.label === 'System' && live.update?.available}
			<span class="size-2 rounded-full bg-brand" title="presso {live.update.latest} is available"
			></span>
		{/if}
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{/snippet}

<div class="flex h-dvh overflow-hidden bg-ground text-ink">
	{#if navOpen}
		<button
			type="button"
			class="fixed inset-0 z-40 bg-scrim lg:hidden"
			aria-label="Close navigation"
			onclick={() => (navOpen = false)}
		></button>
	{/if}
	<aside
		class="fixed inset-y-0 left-0 z-50 flex h-full w-[260px] shrink-0 flex-col gap-5 overflow-y-auto border-r border-rule bg-surface px-3 pt-4 pb-3 transition-transform duration-200 ease-out lg:static lg:translate-x-0 {navOpen
			? 'translate-x-0 shadow-dialog'
			: '-translate-x-full lg:shadow-none'}"
		aria-label="Navigation"
	>
		<div class="flex h-9 items-center justify-between px-2">
			<a href={resolve('/')} class="flex items-center" aria-label="presso home">
				<Wordmark />
			</a>
			<Button
				variant="ghost"
				size="sm"
				icon="close"
				class="-mr-2 lg:hidden"
				aria-label="Close navigation"
				onclick={() => (navOpen = false)}
			/>
		</div>

		<nav class="flex flex-col gap-1" aria-label="Sections">
			{#each primary as item (item.label)}
				{@render navLink(item)}
			{/each}
		</nav>

		<div class="mt-auto flex flex-col gap-2">
			<nav class="flex flex-col gap-1" aria-label="Host">
				{@render navLink(system)}
			</nav>
			<Menu
				items={accountItems}
				label="Account"
				align="left"
				triggerClass="flex w-full items-center gap-3 rounded-control-sm px-2 py-2 text-left transition-colors hover:bg-sunken aria-expanded:bg-sunken"
			>
				{#snippet header()}
					<p class="truncate text-sm font-medium text-ink">{user?.name || user?.email || '–'}</p>
					<p class="truncate text-xs text-ink-2">
						{user?.role === 'admin' ? 'Administrator' : 'Member'}
					</p>
				{/snippet}
				{#snippet children({ open })}
					<span
						class="grid size-9 shrink-0 place-items-center rounded-control-sm bg-ink text-xs font-semibold text-surface"
					>
						{initials}
					</span>
					<span class="min-w-0 flex-1">
						<span class="block truncate text-sm font-medium text-ink"
							>{user?.name || firstName}</span
						>
						<span class="block truncate text-xs text-ink-2">{user?.email || 'Account'}</span>
					</span>
					<Icon
						name="chevron-down"
						size={16}
						class="shrink-0 text-ink-3 transition-transform duration-150 {open ? 'rotate-180' : ''}"
					/>
				{/snippet}
			</Menu>
		</div>
	</aside>

	<div class="flex h-full min-w-0 flex-1 flex-col">
		<header
			class="z-30 flex h-16 shrink-0 items-center gap-3 border-b border-rule bg-surface px-4 sm:gap-4 sm:px-6 lg:pl-8"
		>
			<Button
				variant="ghost"
				size="sm"
				icon="menu"
				class="-ml-1 lg:hidden"
				aria-label="Open navigation"
				aria-expanded={navOpen}
				onclick={() => (navOpen = true)}
			/>
			<div class="flex min-w-0 items-center gap-2 text-sm text-ink-3">
				{#each crumbs as c, i (i)}
					{#if i > 0}<Icon name="chevron-right" size={14} />{/if}
					<span class="truncate {i === crumbs.length - 1 ? 'font-medium text-ink' : ''}">{c}</span>
				{/each}
			</div>

			<Button
				variant="ghost"
				size="sm"
				icon="search"
				class="ml-auto md:hidden"
				aria-label="Search"
				onclick={() => (paletteOpen = true)}
			/>
			<button
				type="button"
				onclick={() => (paletteOpen = true)}
				class="ml-auto hidden h-10 w-[360px] items-center gap-2.5 rounded-control-sm bg-sunken pr-3 pl-3.5 text-left text-sm text-ink-3 transition-colors hover:bg-rule/70 md:flex"
			>
				<Icon name="search" size={18} />
				<span class="flex-1">Search sites, jobs, domains</span>
				<kbd
					class="rounded-key border border-rule bg-surface px-1.5 py-0.5 font-sans text-2xs text-ink-2"
					>⌘K</kbd
				>
			</button>
		</header>

		<main bind:this={mainEl} class="min-h-0 flex-1 overflow-y-auto overscroll-none">
			<div class="w-full max-w-[1400px] px-5 pt-6 pb-16 sm:px-8 sm:pt-9 lg:px-12">
				{#if live.restarting}
					<div
						class="mb-6 flex items-center gap-3 rounded-control border border-rule bg-sunken px-4 py-3 text-sm text-ink"
						role="status"
					>
						<span class="size-2 breathe rounded-full bg-busy"></span>
						presso is restarting into {live.restarting}. The app reloads on its own once the engine
						and the edge are back.
					</div>
				{/if}
				{#if data.apiDown}
					<div class="mx-auto mt-24 max-w-md text-center">
						<h1 class="display text-3xl">The engine is not answering</h1>
						<p class="mt-3 text-sm text-ink-2">
							The presso engine did not respond. Check that it is running on this host, then try
							again.
						</p>
						<div class="mt-6">
							<Button variant="primary" onclick={() => window.location.reload()}>Try again</Button>
						</div>
					</div>
				{:else}
					{@render children()}
				{/if}
			</div>
		</main>
	</div>
</div>

<CommandPalette bind:open={paletteOpen} />
<CreateSiteModal />
