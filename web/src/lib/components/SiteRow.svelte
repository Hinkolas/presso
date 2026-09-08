<!-- A site in the list: the plate laid out sideways. Small preview, name and
     domain, the status, then the hour's numbers as self-labelled cells that
     line up down the list. A site that is not serving puts what it is doing
     instead where the numbers would be: the failure, "Stopped 3 h ago" with
     a Start link, or the setup step and its bar.

     Columns shed with the list's width (container queries on the list):
     p95 needs 56rem, the runtime 64rem. Below 44rem the page shows the grid
     card instead, so the row never has to fold. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createSite } from '$lib/state/create-site.svelte';
	import { jobEvents, jobFor } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runSiteAction, shownStatus } from '$lib/site-actions';
	import { isLive, jobProgress } from '$lib/job-progress';
	import { cacheRatio, formatDuration, formatNumber, formatRelative } from '$lib/format';
	import { branchOf } from '$lib/state/capabilities.svelte';
	import type { MetricsResponse, Site, SiteAction } from '$lib/types/site';
	import Icon from './Icon.svelte';
	import Menu, { type MenuItem } from './Menu.svelte';
	import ProgressBar from './ProgressBar.svelte';
	import RuntimeBadge from './RuntimeBadge.svelte';
	import StatusPill from './StatusPill.svelte';
	import Thumb from './Thumb.svelte';

	let { site, hour }: { site: Site; hour?: MetricsResponse } = $props();

	const href = $derived(resolve(`/sites/${site.id}`));
	const m = $derived(site.metrics_1h);
	const shown = $derived(shownStatus(site));
	const running = $derived(site.status === 'running');
	const wp = $derived(branchOf(site.wp_version ?? ''));

	const setup = $derived(site.status === 'provisioning' || site.status === 'deleting');
	const job = $derived.by(() => {
		if (!setup) return null;
		const j = jobFor(site.id);
		return j && isLive(j) ? j : null;
	});
	const progress = $derived(job ? jobProgress(job, jobEvents.get(job.id) ?? []) : null);

	let el: HTMLElement | undefined = $state();
	$effect(() => {
		if (createSite.justCreated !== site.id || !el) return;
		el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
		createSite.justCreated = null;
	});
	// p95 for the hour: the worst minute that saw traffic.
	const p95 = $derived.by(() => {
		const buckets = (hour?.requests ?? []).filter((b) => b.requests > 0);
		if (!buckets.length) return null;
		return Math.max(...buckets.map((b) => b.dur_p95_ms));
	});

	let busy = $state(false);
	async function act(a: SiteAction) {
		busy = true;
		try {
			await runSiteAction(site, a);
		} catch {
			// The site page shows the error in full.
		} finally {
			busy = false;
		}
	}

	const items = $derived.by<MenuItem[]>(() => {
		const out: MenuItem[] = [
			{ label: 'Overview', icon: 'sites', href },
			{ label: 'Open site', icon: 'external', href: site.url, external: true },
			{ label: 'Settings', icon: 'settings', href: `${href}?tab=settings` }
		];
		if (isAdmin()) {
			out.push({ divider: true });
			if (running) {
				out.push({
					label: 'Restart',
					icon: 'refresh',
					onclick: () => act('restart'),
					disabled: busy
				});
				out.push({ label: 'Stop', icon: 'stop', onclick: () => act('stop'), disabled: busy });
				out.push({ label: 'Purge page cache', icon: 'broom', onclick: () => act('purge-cache') });
			} else if (site.status === 'stopped') {
				out.push({ label: 'Start', icon: 'play', onclick: () => act('start'), disabled: busy });
			} else if (site.status === 'error') {
				out.push({ label: 'Retry', icon: 'refresh', onclick: () => act('retry'), disabled: busy });
			}
		}
		return out;
	});

	function openRow(e: MouseEvent) {
		// Plain clicks on the row open the site; links, buttons and menus keep their own behaviour.
		if ((e.target as HTMLElement).closest('a, button')) return;
		goto(href);
	}

	// One of the hour's numbers: value over its caption, right-aligned so the
	// column reads down the list.
	const cell = 'flex w-18 shrink-0 flex-col items-end text-right';
	const value = 'text-md leading-6 font-semibold num text-ink';
	const caption = 'text-2xs leading-4 text-ink-3 whitespace-nowrap';
</script>

<!-- The whole row opens the site for the pointer; the keyboard has the name
     link and the menu, so no key handler is bolted on. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<article
	bind:this={el}
	class="flex cursor-pointer items-center gap-5 frame py-4 pr-4 pl-4 transition-shadow duration-200 hover:shadow-plate-hover"
	onclick={openRow}
>
	<a {href} class="block shrink-0" tabindex="-1" aria-hidden="true">
		<Thumb {site} size="sm" class="h-[75px] w-[120px] rounded-chip ring-1 ring-rule" />
	</a>

	<div class="min-w-0 flex-1">
		<a {href} class="block truncate display text-[22px] leading-[1.1] text-ink hover:text-brand"
			>{site.name}</a
		>
		<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
		<a
			href={site.url}
			target="_blank"
			rel="noreferrer noopener"
			class="mt-1 inline-flex max-w-full items-center gap-1.5 text-xs text-ink-2 hover:text-ink"
		>
			<span class="truncate">{site.primary_domain}</span>
			<Icon name="external" size={12} class="text-ink-3" />
		</a>
		<!-- eslint-enable svelte/no-navigation-without-resolve -->
	</div>

	<div class="w-32 shrink-0">
		<StatusPill status={shown} soft />
	</div>

	<!-- The hour's numbers, or what the site is doing instead of serving. The
	     box is as wide as the cells so the pills line up down the list. -->
	<div class="flex w-41 shrink-0 items-center justify-end gap-5 @4xl:w-64">
		{#if running}
			<div class={cell}>
				<span class={value}>{formatNumber(m?.requests ?? 0)}</span>
				<span class={caption}>requests, 1 h</span>
			</div>
			<div class={cell}>
				<span class={value}>{m && m.requests ? cacheRatio(m) : '–'}</span>
				<span class={caption}>from cache</span>
			</div>
			<div class="{cell} hidden @4xl:flex">
				<span class={value}>{p95 !== null ? formatDuration(p95) : '–'}</span>
				<span class={caption}>p95</span>
			</div>
		{:else if setup}
			<div class="flex w-full flex-col gap-1.5">
				<span class="truncate text-xs text-ink-2">
					{#if progress?.current}
						{progress.current.label}{progress.total
							? ` · ${Math.min(progress.position + 1, progress.total)} of ${progress.total}`
							: ''}
					{:else if job}
						Waiting for a worker
					{:else}
						{site.status_message || 'In progress'}
					{/if}
				</span>
				<ProgressBar
					size="sm"
					value={progress?.total ? progress.fraction : null}
					label="{site.status === 'deleting' ? 'Deleting' : 'Setting up'} {site.name}"
				/>
			</div>
		{:else if site.status === 'error'}
			<span class="max-w-full truncate text-sm text-danger-ink" title={site.status_message}
				>{site.status_message || 'Something went wrong'}</span
			>
		{:else if site.status === 'stopped'}
			<span class="flex max-w-full items-center gap-3 text-sm whitespace-nowrap text-ink-2">
				<span class="truncate">Stopped {formatRelative(site.updated_at)}</span>
				{#if isAdmin()}
					<button
						type="button"
						class="font-medium text-brand hover:text-brand-hover disabled:opacity-50"
						onclick={() => act('start')}
						disabled={busy}>Start</button
					>
				{/if}
			</span>
		{/if}
	</div>

	<div class="hidden w-40 shrink-0 flex-col items-end text-right @5xl:flex">
		<span class="flex items-center gap-2 text-sm leading-6 whitespace-nowrap text-ink-2 num">
			<RuntimeBadge runtime={site.runtime} />
			{wp ? `WP ${wp} · ` : ''}PHP {site.php_version}
		</span>
		<span class={caption}>runtime</span>
	</div>

	<Menu {items} size="sm" label="Actions for {site.name}" />
</article>
