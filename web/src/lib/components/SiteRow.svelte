<!-- A site in the list: small preview, name, status, and the hour's numbers
     as right-aligned columns. While it is being set up or torn down the
     status cell carries the step under way and a bar. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { createSite } from '$lib/state/create-site.svelte';
	import { jobEvents, jobFor } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runSiteAction, shownStatus } from '$lib/site-actions';
	import { isLive, jobProgress } from '$lib/job-progress';
	import { cacheRatio, formatDuration, formatNumber } from '$lib/format';
	import { branchOf } from '$lib/state/capabilities.svelte';
	import type { MetricsResponse, Site, SiteAction } from '$lib/types/site';
	import Icon from './Icon.svelte';
	import Menu, { type MenuItem } from './Menu.svelte';
	import ProgressBar from './ProgressBar.svelte';
	import RuntimeBadge from './RuntimeBadge.svelte';
	import StatusPill from './StatusPill.svelte';
	import Thumb from './Thumb.svelte';
	import { td } from './Table.svelte';

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

	let el: HTMLTableRowElement | undefined = $state();
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
</script>

<tr bind:this={el} class="cursor-pointer transition-colors hover:bg-sunken/50" onclick={openRow}>
	<td class="{td} py-3">
		<div class="flex items-center gap-[18px]">
			<a {href} class="block shrink-0" tabindex="-1" aria-hidden="true">
				<Thumb {site} size="sm" class="h-[75px] w-[120px] rounded-chip ring-1 ring-rule" />
			</a>
			<div class="min-w-0">
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
		</div>
	</td>
	<td class={td}>
		<div class="flex flex-col items-start gap-1.5">
			<StatusPill status={shown} soft />
			{#if site.status === 'error' && site.status_message}
				<span class="max-w-44 truncate text-xs text-ink-2" title={site.status_message}>
					{site.status_message}
				</span>
			{:else if setup}
				<ProgressBar
					class="mt-0.5 w-44"
					size="sm"
					value={progress?.total ? progress.fraction : null}
					label="{site.status === 'deleting' ? 'Deleting' : 'Setting up'} {site.name}"
				/>
				<span class="max-w-44 truncate text-xs text-ink-2">
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
			{/if}
		</div>
	</td>
	<td class="{td} text-right font-semibold num">
		{#if running}{formatNumber(m?.requests ?? 0)}{:else}<span class="text-ink-3">–</span>{/if}
	</td>
	<td class="{td} text-right text-ink-2 num">
		{#if running && m && m.requests}{cacheRatio(m)}{:else}<span class="text-ink-3">–</span>{/if}
	</td>
	<td class="{td} text-right text-ink-2 num">
		{#if running && p95 !== null}{formatDuration(p95)}{:else}<span class="text-ink-3">–</span>{/if}
	</td>
	<td class="{td} text-right">
		<div class="flex flex-col items-end gap-1">
			<RuntimeBadge runtime={site.runtime} />
			<span class="text-ink-2 num">{wp ? `WP ${wp} · ` : ''}PHP {site.php_version}</span>
		</div>
	</td>
	<td class="{td} w-16 pr-4 text-right">
		<Menu {items} size="sm" label="Actions for {site.name}" />
	</td>
</tr>
