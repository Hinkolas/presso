<!-- App shell: a sidebar for navigation (with a Platform section that has
     room to grow) and platform health, a topbar for search and the account,
     and the page below. One event stream is opened here and shared by every
     page. -->
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
		},
		{ href: resolve('/jobs'), label: 'Jobs', icon: 'jobs', match: (p) => p.startsWith('/jobs') }
	];
	const platform: NavItem[] = [
		{ href: resolve('/system') + '#edge', label: 'Edge', icon: 'edge', match: () => false },
		{
			href: resolve('/system') + '#database',
			label: 'Database',
			icon: 'database',
			match: () => false
		},
		{ href: resolve('/system') + '#cache', label: 'Cache', icon: 'cache', match: () => false },
		{
			href: resolve('/system'),
			label: 'System',
			icon: 'system',
			match: (p) => p.startsWith('/system')
		}
	];

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

	// Platform health for the sidebar chip: every component reported ok. A
	// dropped event stream is surfaced here too, since the data on screen may
	// be stale until it reconnects.
	const platformState = $derived.by(() => {
		if (!live.connected && live.generation > 0) return 'offline';
		const comps = live.platform;
		if (!comps || comps.length === 0) return null;
		const bad = comps.some(
			(c) => c.error || c.state === 'error' || c.state === 'missing' || c.health === 'unhealthy'
		);
		return bad ? 'error' : 'ok';
	});

	// Topbar context: the section, then the page within it.
	const crumbs = $derived.by(() => {
		const p = page.url.pathname;
		if (p.startsWith('/sites/')) return ['Sites', sites.get(page.params.id ?? '')?.name ?? 'Site'];
		if (p.startsWith('/jobs/')) return ['Jobs', 'Job'];
		if (p.startsWith('/jobs')) return ['Jobs'];
		if (p.startsWith('/system')) return ['System'];
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
		{:else if item.label === 'Jobs' && runningJobs}
			<span class="size-2 breathe rounded-full bg-busy" title="{runningJobs} running"></span>
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
		class="fixed inset-y-0 left-0 z-50 flex h-full w-[260px] shrink-0 flex-col gap-[26px] overflow-y-auto border-r border-rule bg-surface px-4 pt-[22px] pb-5 transition-transform duration-200 ease-out lg:static lg:translate-x-0 {navOpen
			? 'translate-x-0 shadow-dialog'
			: '-translate-x-full lg:shadow-none'}"
		aria-label="Navigation"
	>
		<div class="flex h-9 items-center justify-between px-2.5">
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

		<div class="flex flex-col gap-1">
			<p class="px-3 pb-2 text-xs text-ink-3">Platform</p>
			{#each platform as item (item.label)}
				{@render navLink(item)}
			{/each}
		</div>

		<div class="mt-auto">
			<a
				href={resolve('/system')}
				class="flex items-center gap-2.5 rounded-control bg-sunken px-3.5 py-3 text-sm text-ink-2 transition-colors hover:bg-rule/70 hover:text-ink"
			>
				<span
					class="size-2 rounded-full {platformState === 'ok'
						? 'bg-ok'
						: platformState === 'error'
							? 'bg-danger'
							: platformState === 'offline'
								? 'breathe bg-busy'
								: 'bg-rule-strong'}"
				></span>
				<span>
					{#if platformState === 'ok'}
						All systems healthy
					{:else if platformState === 'error'}
						A component needs attention
					{:else if platformState === 'offline'}
						Reconnecting live updates
					{:else}
						Checking the platform
					{/if}
				</span>
			</a>
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

			<Menu
				items={accountItems}
				label="Account"
				align="right"
				triggerClass="flex items-center gap-2.5 rounded-full py-0.5 pr-1.5 pl-0.5 text-left transition-colors hover:bg-sunken aria-expanded:bg-sunken"
			>
				{#snippet header()}
					<p class="truncate text-sm font-medium text-ink">{user?.name || user?.email || '–'}</p>
					<p class="truncate text-xs text-ink-2">
						{user?.name ? user.email : ''}{user?.role === 'admin' ? ' · administrator' : ''}
					</p>
				{/snippet}
				{#snippet children({ open })}
					<span
						class="grid size-[34px] place-items-center rounded-full bg-ink text-xs font-semibold text-surface"
					>
						{initials}
					</span>
					<span class="hidden text-sm font-medium text-ink sm:block">{firstName}</span>
					<Icon
						name="chevron-down"
						size={16}
						class="text-ink-3 transition-transform duration-150 {open ? 'rotate-180' : ''}"
					/>
				{/snippet}
			</Menu>
		</header>

		<main bind:this={mainEl} class="min-h-0 flex-1 overflow-y-auto overscroll-none">
			<div class="w-full max-w-[1400px] px-5 pt-6 pb-16 sm:px-8 sm:pt-9 lg:px-12">
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
