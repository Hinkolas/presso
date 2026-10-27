<!-- Settings: the host's sections (System, Updates) in a side column on
     wide screens and a row of links above the page on narrow ones. Each
     section renders its own page header. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { live } from '$lib/state/live.svelte';
	import Icon, { type IconName } from '$lib/components/Icon.svelte';

	let { children }: { children: Snippet } = $props();

	type Section = { href: string; label: string; icon: IconName; match: string };
	const sections: Section[] = [
		{
			href: resolve('/(app)/settings/system'),
			label: 'System',
			icon: 'system',
			match: '/settings/system'
		},
		{
			href: resolve('/(app)/settings/updates'),
			label: 'Updates',
			icon: 'refresh',
			match: '/settings/updates'
		}
	];
	const updateBusy = $derived(!!live.update?.job_id || !!live.restarting);
</script>

<div class="flex flex-col gap-6 lg:flex-row lg:gap-10">
	<nav
		class="flex [scrollbar-width:none] gap-1 overflow-x-auto lg:w-52 lg:shrink-0 lg:flex-col"
		aria-label="Settings"
	>
		<p class="hidden px-3 pb-2 text-xs font-medium tracking-wide text-ink-3 uppercase lg:block">
			Settings
		</p>
		{#each sections as s (s.href)}
			{@const active = page.url.pathname.startsWith(s.match)}
			<!-- eslint-disable svelte/no-navigation-without-resolve -- resolved above -->
			<a
				href={s.href}
				class="flex h-10 shrink-0 items-center gap-3 rounded-control-sm px-3 text-sm font-medium transition-colors {active
					? 'bg-sunken text-ink'
					: 'text-ink-2 hover:bg-sunken hover:text-ink'}"
				aria-current={active ? 'page' : undefined}
			>
				<Icon name={s.icon} size={18} class={active ? 'text-ink' : 'text-ink-3'} />
				<span class="flex-1">{s.label}</span>
				{#if s.label === 'Updates' && updateBusy}
					<span class="size-2 breathe rounded-full bg-busy" title="Update in progress"></span>
				{:else if s.label === 'Updates' && live.update?.available}
					<span
						class="size-2 rounded-full bg-brand"
						title="presso {live.update.latest} is available"
					></span>
				{/if}
			</a>
			<!-- eslint-enable svelte/no-navigation-without-resolve -->
		{/each}
	</nav>
	<div class="min-w-0 flex-1">
		{@render children()}
	</div>
</div>
