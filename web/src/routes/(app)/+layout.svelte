<!-- App shell: sidebar navigation, user menu, and the single live event
     stream every page merges into its own data. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import type { Pathname } from '$app/types';
	import { api } from '$lib/api/client';
	import { connectLive, disconnectLive, live } from '$lib/state/live.svelte';
	import { session, setUser } from '$lib/state/session.svelte';
	import Button from '$lib/components/Button.svelte';
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: Snippet } = $props();

	const user = $derived(session.user ?? data.user);

	const nav: { href: Pathname; label: string; match: (p: string) => boolean }[] = [
		{ href: '/', label: 'Sites', match: (p: string) => p === '/' || p.startsWith('/sites') },
		{ href: '/jobs', label: 'Jobs', match: (p: string) => p.startsWith('/jobs') },
		{ href: '/system', label: 'System', match: (p: string) => p.startsWith('/system') }
	];

	let menuOpen = $state(false);

	$effect(() => {
		if (data.apiDown) return;
		connectLive();
		return () => disconnectLive();
	});

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
</script>

<svelte:window onclick={closeMenu} />

<div class="flex min-h-screen bg-gray-50 text-gray-900">
	<aside class="flex w-56 shrink-0 flex-col border-r border-gray-200 bg-white">
		<div class="flex items-center gap-2 px-5 py-4">
			<a href={resolve('/')} class="text-lg font-bold tracking-tight">presso</a>
			<span
				class="ml-auto inline-flex items-center gap-1 text-[10px] tracking-wide uppercase {live.connected
					? 'text-green-700'
					: 'text-gray-400'}"
				title={live.connected ? 'Live updates connected' : 'Live updates disconnected'}
			>
				<span class="size-1.5 rounded-full {live.connected ? 'bg-green-500' : 'bg-gray-300'}"
				></span>
				live
			</span>
		</div>
		<nav class="flex flex-1 flex-col gap-0.5 px-3">
			{#each nav as item (item.href)}
				{@const active = item.match(page.url.pathname)}
				<a
					href={resolve(item.href)}
					class="rounded-md px-2.5 py-1.5 text-sm font-medium {active
						? 'bg-gray-100 text-gray-900'
						: 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'}"
					aria-current={active ? 'page' : undefined}
				>
					{item.label}
				</a>
			{/each}
		</nav>
		<div class="relative border-t border-gray-200 p-3" data-user-menu>
			{#if menuOpen}
				<div
					class="absolute bottom-full left-3 mb-1 w-48 rounded-md border border-gray-200 bg-white py-1 shadow-lg"
				>
					<a
						href={resolve('/account')}
						onclick={() => (menuOpen = false)}
						class="block px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50">Account</a
					>
					<button
						type="button"
						onclick={signOut}
						class="block w-full px-3 py-1.5 text-left text-sm text-gray-700 hover:bg-gray-50"
					>
						Sign out
					</button>
				</div>
			{/if}
			<button
				type="button"
				onclick={() => (menuOpen = !menuOpen)}
				class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-gray-50"
				aria-haspopup="menu"
				aria-expanded={menuOpen}
			>
				<span
					class="flex size-7 shrink-0 items-center justify-center rounded-full bg-gray-200 text-xs font-semibold uppercase"
				>
					{(user?.name || user?.email || '?').slice(0, 1)}
				</span>
				<span class="min-w-0 flex-1">
					<span class="block truncate text-sm font-medium">{user?.name || user?.email || '–'}</span>
					{#if user?.name}
						<span class="block truncate text-xs text-gray-500">{user.email}</span>
					{/if}
				</span>
			</button>
		</div>
	</aside>

	<main class="min-w-0 flex-1 px-8 py-6">
		{#if data.apiDown}
			<div class="mx-auto mt-24 max-w-md text-center">
				<h1 class="text-lg font-semibold">The server is unreachable</h1>
				<p class="mt-2 text-sm text-gray-500">
					The presso engine did not answer. Check that it is running, then try again.
				</p>
				<div class="mt-4">
					<Button variant="primary" onclick={() => window.location.reload()}>Retry</Button>
				</div>
			</div>
		{:else}
			{@render children()}
		{/if}
	</main>
</div>
