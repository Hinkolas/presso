<!-- A site in the grid: its front page, its name, one line of numbers. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { jobs } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runSiteAction, shownStatus } from '$lib/site-actions';
	import { formatNumber, formatRelative, percent } from '$lib/format';
	import type { Site } from '$lib/types/site';
	import Icon from './Icon.svelte';
	import StatusPill from './StatusPill.svelte';
	import Thumb from './Thumb.svelte';

	let { site }: { site: Site } = $props();

	const href = $derived(resolve(`/sites/${site.id}`));
	const m = $derived(site.metrics_1h);
	const shown = $derived(shownStatus(site));
	const activeJob = $derived.by(() => {
		for (const j of jobs.values()) {
			if (j.site_id === site.id && (j.status === 'queued' || j.status === 'running')) return j;
		}
		return null;
	});

	let busy = $state(false);
	async function start() {
		busy = true;
		try {
			await runSiteAction(site, 'start');
		} catch {
			// The site page shows the error in full.
		} finally {
			busy = false;
		}
	}
</script>

<article
	class="group flex flex-col overflow-hidden frame transition-shadow duration-200 hover:shadow-plate-hover"
>
	<a {href} class="block" aria-label="Open {site.name}">
		<Thumb {site} class="aspect-[16/10]" />
	</a>

	<div class="flex flex-col gap-4 border-t border-rule px-6 pt-5 pb-[22px]">
		<div class="flex items-start justify-between gap-4">
			<div class="min-w-0">
				<a {href} class="block truncate display text-[26px] leading-[1.1] text-ink hover:text-brand"
					>{site.name}</a
				>
				<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
				<a
					href={site.url}
					target="_blank"
					rel="noreferrer noopener"
					class="mt-1 inline-flex max-w-full items-center gap-1.5 text-sm text-ink-2 hover:text-ink"
				>
					<span class="truncate">{site.primary_domain}</span>
					<Icon name="external" size={13} class="text-ink-3" />
				</a>
				<!-- eslint-enable svelte/no-navigation-without-resolve -->
			</div>
			<StatusPill status={shown} soft />
		</div>

		<div class="flex items-center gap-6 text-sm text-ink-2">
			{#if site.status === 'provisioning' || site.status === 'deleting'}
				<span>Started {formatRelative(site.updated_at)}</span>
				{#if activeJob}
					<a
						href={resolve(`/jobs/${activeJob.id}`)}
						class="font-medium text-brand hover:text-brand-hover">Follow the job</a
					>
				{/if}
			{:else if site.status === 'stopped'}
				<span>Stopped {formatRelative(site.updated_at)}</span>
				{#if isAdmin()}
					<button
						type="button"
						class="font-medium text-brand hover:text-brand-hover disabled:opacity-50"
						onclick={start}
						disabled={busy}>Start</button
					>
				{/if}
			{:else if site.status === 'error'}
				<span class="truncate text-danger-ink" title={site.status_message}
					>{site.status_message || 'Something went wrong'}</span
				>
			{:else}
				<span class="num"
					><strong class="font-semibold text-ink">{formatNumber(m?.requests ?? 0)}</strong> requests this
					hour</span
				>
				<span class="num"
					><strong class="font-semibold text-ink"
						>{m && m.requests ? percent(m.cache_hit + m.cache_stale, m.requests) : '–'}</strong
					> from cache</span
				>
				{#if m?.status_5xx}
					<span class="font-medium text-danger-ink num">{formatNumber(m.status_5xx)} errors</span>
				{/if}
			{/if}
			<span class="ml-auto shrink-0 text-ink-3 num">PHP {site.php_version}</span>
		</div>
	</div>
</article>
