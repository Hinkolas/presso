<!-- ⌘K: jump to a site or a section. Sites come from the API when the
     palette opens; the live map keeps names current. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import type { Pathname } from '$app/types';
	import { api } from '$lib/api/client';
	import { openCreateSite } from '$lib/state/create-site.svelte';
	import { sites as liveSites } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import type { Site } from '$lib/types/site';
	import Icon, { type IconName } from './Icon.svelte';
	import Lamp from './Lamp.svelte';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	type Entry = {
		id: string;
		title: string;
		detail?: string;
		icon?: IconName;
		status?: string;
		href?: string;
		external?: boolean;
		/** Runs instead of navigating to `href`. */
		onselect?: () => void;
	};

	let el: HTMLDialogElement | undefined = $state();
	let input: HTMLInputElement | undefined = $state();
	let query = $state('');
	let cursor = $state(0);
	let loaded = $state<Site[]>([]);

	$effect(() => {
		if (!el) return;
		if (open && !el.open) {
			query = '';
			cursor = 0;
			el.showModal();
			queueMicrotask(() => input?.focus());
			api
				.get<{ sites: Site[] }>('/v1/sites')
				.then((r) => (loaded = r.sites))
				.catch(() => {});
		} else if (!open && el.open) el.close();
	});

	const sections: Entry[] = [
		{ id: 'nav-sites', title: 'Sites', icon: 'sites', href: resolve('/') },
		{ id: 'nav-jobs', title: 'Activity', icon: 'jobs', href: resolve('/jobs') },
		{ id: 'nav-system', title: 'System', icon: 'system', href: resolve('/system') },
		{ id: 'nav-account', title: 'Account security', icon: 'user', href: resolve('/account') }
	];

	const all = $derived.by<Entry[]>(() => {
		const byId: Record<string, Site> = {};
		for (const s of loaded) byId[s.id] = s;
		for (const s of liveSites.values()) byId[s.id] = s;
		const sites = Object.values(byId)
			.sort((a, b) => a.name.localeCompare(b.name))
			.map<Entry>((s) => ({
				id: s.id,
				title: s.name,
				detail: s.primary_domain,
				status: s.status,
				href: resolve(`/sites/${s.id}`)
			}));
		const nav = [...sections];
		if (isAdmin())
			nav.push({ id: 'nav-new', title: 'New site', icon: 'plus', onselect: openCreateSite });
		return [...sites, ...nav];
	});

	const results = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return all;
		return all.filter(
			(e) => e.title.toLowerCase().includes(q) || (e.detail ?? '').toLowerCase().includes(q)
		);
	});
	$effect(() => {
		if (cursor >= results.length) cursor = Math.max(0, results.length - 1);
	});

	function pick(e: Entry) {
		open = false;
		if (e.onselect) e.onselect();
		else if (!e.href) return;
		else if (e.external) window.open(e.href, '_blank', 'noopener');
		// eslint-disable-next-line svelte/no-navigation-without-resolve -- hrefs are resolved above
		else goto(e.href as Pathname);
	}

	function onKey(ev: KeyboardEvent) {
		if (ev.key === 'ArrowDown') {
			ev.preventDefault();
			cursor = Math.min(results.length - 1, cursor + 1);
		} else if (ev.key === 'ArrowUp') {
			ev.preventDefault();
			cursor = Math.max(0, cursor - 1);
		} else if (ev.key === 'Enter') {
			ev.preventDefault();
			const e = results[cursor];
			if (e) pick(e);
		}
	}
</script>

<dialog
	bind:this={el}
	onclose={() => (open = false)}
	onclick={(e) => {
		if (e.target === el) open = false;
	}}
	class="mx-auto mt-[12vh] w-[min(40rem,calc(100vw-2rem))] rounded-dialog bg-surface p-0 text-ink shadow-dialog backdrop:bg-scrim"
>
	{#if open}
		<div class="flex items-center gap-3 border-b border-rule px-5">
			<Icon name="search" size={20} class="text-ink-3" />
			<input
				bind:this={input}
				bind:value={query}
				onkeydown={onKey}
				oninput={() => (cursor = 0)}
				class="h-14 flex-1 bg-transparent text-base text-ink outline-none placeholder:text-ink-3"
				placeholder="Search sites, jobs, domains"
				aria-label="Search"
				autocomplete="off"
				spellcheck="false"
			/>
			<kbd
				class="rounded-key border border-rule bg-sunken px-1.5 py-0.5 font-sans text-2xs text-ink-3"
				>esc</kbd
			>
		</div>
		<ul class="max-h-[50vh] overflow-y-auto p-2" role="listbox">
			{#each results as e, i (e.id)}
				<li>
					<button
						type="button"
						role="option"
						aria-selected={i === cursor}
						onmouseenter={() => (cursor = i)}
						onclick={() => pick(e)}
						class="flex w-full items-center gap-3 rounded-control-sm px-3 py-2.5 text-left {i ===
						cursor
							? 'bg-sunken'
							: ''}"
					>
						{#if e.status}
							<span class="grid size-5 place-items-center"><Lamp status={e.status} size={8} /></span
							>
						{:else if e.icon}
							<Icon name={e.icon} size={18} class="text-ink-3" />
						{/if}
						<span class="min-w-0 flex-1">
							<span class="block truncate text-sm font-medium text-ink">{e.title}</span>
							{#if e.detail}
								<span class="block truncate text-xs text-ink-3">{e.detail}</span>
							{/if}
						</span>
						{#if i === cursor}
							<kbd class="font-sans text-2xs text-ink-3">↵</kbd>
						{/if}
					</button>
				</li>
			{:else}
				<li class="px-3 py-6 text-center text-sm text-ink-3">Nothing matches “{query}”.</li>
			{/each}
		</ul>
	{/if}
</dialog>
