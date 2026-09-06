<!-- App shell: a sidebar for navigation (with a Platform section that has
     room to grow), a topbar for search, live state, jobs and the account,
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
	import Icon, { type IconName } from '$lib/components/Icon.svelte';
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

	// Platform health for the sidebar chip: every component reported ok.
	const platformState = $derived.by(() => {
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
		if (p.startsWith('/sites/new')) return ['Sites', 'New site'];
		if (p.startsWith('/sites/')) return ['Sites', sites.get(page.params.id ?? '')?.name ?? 'Site'];
		if (p.startsWith('/jobs/')) return ['Jobs', 'Job'];
		if (p.startsWith('/jobs')) return ['Jobs'];
		if (p.startsWith('/system')) return ['System'];
		if (p.startsWith('/account')) return ['Account security'];
		return ['Sites'];
	});

	let menuOpen = $state(false);
	let paletteOpen = $state(false);
	let mainEl: HTMLElement | undefined = $state();

	// The content pane is the scroller, so a new page starts at the top
	// (tab switches change only the query and keep their position).
	$effect(() => {
		void page.url.pathname;
		mainEl?.scrollTo({ top: 0 });
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
		}
	}

	async function signOut() {
		menuOpen = false;
		try {
			await api.post('/v1/auth/logout');
		} catch {
			// The API's session sweeper collects leftovers.
		}
		setUser(null);
		await goto(resolve('/auth/login'));
	}

	function closeMenu(e: MouseEvent) {
		if (!(e.target as HTMLElement).closest('[data-user-menu]')) menuOpen = false;
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
</script>

<svelte:window onclick={closeMenu} onkeydown={onKey} />

{#snippet navLink(item: NavItem)}
	{@const active = item.match(page.url.pathname)}
	<!-- eslint-disable svelte/no-navigation-without-resolve -- resolved above -->
	<a
		href={item.href}
		class="flex h-11 items-center gap-3 rounded-[10px] px-3 text-base font-medium transition-colors {active
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
	<aside
		class="flex h-full w-[260px] shrink-0 flex-col gap-[26px] overflow-y-auto border-r border-rule bg-surface px-4 pt-[22px] pb-5"
	>
		<a href={resolve('/')} class="flex h-9 items-center px-2.5" aria-label="presso home">
			<Wordmark />
		</a>

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

		<div class="mt-auto flex flex-col gap-3.5">
			<a
				href={resolve('/system')}
				class="flex items-center gap-2.5 rounded-control bg-sunken px-3.5 py-3 text-sm text-ink-2 hover:text-ink"
			>
				<span
					class="size-2 rounded-full {platformState === 'ok'
						? 'bg-ok'
						: platformState === 'error'
							? 'bg-danger'
							: 'bg-rule-strong'}"
				></span>
				<span>
					{#if platformState === 'ok'}
						All systems healthy
					{:else if platformState === 'error'}
						A component needs attention
					{:else}
						Checking the platform
					{/if}
				</span>
			</a>
			<div class="flex items-center justify-between px-3 text-xs text-ink-3">
				<span>presso</span>
				<span class="num">{live.connected ? 'connected' : 'offline'}</span>
			</div>
		</div>
	</aside>

	<div class="flex h-full min-w-0 flex-1 flex-col">
		<header
			class="z-30 flex h-16 shrink-0 items-center gap-4 border-b border-rule bg-surface px-6 pl-8"
		>
			<div class="flex items-center gap-2 text-sm text-ink-3">
				{#each crumbs as c, i (i)}
					{#if i > 0}<Icon name="chevron-right" size={14} />{/if}
					<span class={i === crumbs.length - 1 ? 'font-medium text-ink' : ''}>{c}</span>
				{/each}
			</div>

			<button
				type="button"
				onclick={() => (paletteOpen = true)}
				class="ml-auto hidden h-10 w-[360px] items-center gap-2.5 rounded-[10px] bg-sunken pr-3 pl-3.5 text-left text-sm text-ink-3 transition-colors hover:bg-rule/70 md:flex"
			>
				<Icon name="search" size={18} />
				<span class="flex-1">Search sites, jobs, domains</span>
				<kbd
					class="rounded-[6px] border border-rule bg-surface px-1.5 py-0.5 font-sans text-2xs text-ink-2"
					>⌘K</kbd
				>
			</button>

			<span
				class="inline-flex h-8 items-center gap-2 rounded-full px-3 text-xs font-medium {live.connected
					? 'bg-ok-soft text-ok-ink'
					: 'bg-sunken text-ink-3'}"
				title={live.connected ? 'Live updates connected' : 'Live updates disconnected'}
			>
				<span class="size-2 rounded-full {live.connected ? 'bg-ok' : 'bg-rule-strong'}"></span>
				{live.connected ? 'Live' : 'Reconnecting'}
			</span>

			<a
				href={resolve('/jobs')}
				class="relative grid size-10 place-items-center rounded-[10px] text-ink-2 hover:bg-sunken hover:text-ink"
				aria-label="Jobs{runningJobs ? `, ${runningJobs} running` : ''}"
				title="Jobs"
			>
				<Icon name="clock" size={20} />
				{#if runningJobs}
					<span
						class="absolute top-1 right-1 grid h-[18px] min-w-[18px] place-items-center rounded-full bg-busy px-1 text-[11px] font-semibold text-white num"
						>{runningJobs}</span
					>
				{/if}
			</a>

			<span class="mx-1 h-6 w-px bg-rule"></span>

			<div class="relative" data-user-menu>
				<button
					type="button"
					onclick={() => (menuOpen = !menuOpen)}
					class="flex items-center gap-2.5 rounded-full py-0.5 pr-1 pl-0.5 text-left hover:bg-sunken"
					aria-haspopup="menu"
					aria-expanded={menuOpen}
				>
					<span
						class="grid size-[34px] place-items-center rounded-full bg-ink text-xs font-semibold text-surface"
					>
						{initials}
					</span>
					<span class="hidden text-sm font-medium text-ink sm:block">{firstName}</span>
					<Icon name="chevron-down" size={16} class="text-ink-3" />
				</button>
				{#if menuOpen}
					<div
						class="absolute top-full right-0 mt-2 w-64 rounded-plate bg-surface p-2 shadow-pop"
						role="menu"
					>
						<div class="px-3 py-2.5">
							<p class="truncate text-sm font-medium text-ink">
								{user?.name || user?.email || '–'}
							</p>
							<p class="truncate text-xs text-ink-2">
								{user?.name ? user.email : ''}{user?.role === 'admin' ? ' · administrator' : ''}
							</p>
						</div>
						<div class="my-1.5 h-px bg-rule"></div>
						<a
							href={resolve('/account')}
							onclick={() => (menuOpen = false)}
							role="menuitem"
							class="flex items-center gap-3 rounded-control-sm px-3 py-2.5 text-sm font-medium text-ink hover:bg-sunken"
						>
							<Icon name="user" size={18} class="text-ink-3" />
							Account security
						</a>
						<button
							type="button"
							onclick={signOut}
							role="menuitem"
							class="flex w-full items-center gap-3 rounded-control-sm px-3 py-2.5 text-left text-sm font-medium text-ink hover:bg-sunken"
						>
							<Icon name="logout" size={18} class="text-ink-3" />
							Sign out
						</button>
					</div>
				{/if}
			</div>
		</header>

		<main bind:this={mainEl} class="min-h-0 flex-1 overflow-y-auto overscroll-none">
			<div class="w-full max-w-[1400px] px-12 pt-9 pb-16">
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
