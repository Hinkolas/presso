<!-- A site in the grid: its front page, its name, one line of numbers. While
     the site is being set up or torn down the preview area shows the job
     instead: the step under way and how far along it is. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { createSite } from '$lib/state/create-site.svelte';
	import { jobEvents, jobFor } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runSiteAction, shownStatus } from '$lib/site-actions';
	import { isLive, jobProgress } from '$lib/job-progress';
	import { cacheRatio, formatDuration, formatNumber, formatRelative, spanMs } from '$lib/format';
	import { branchOf } from '$lib/state/capabilities.svelte';
	import type { Site } from '$lib/types/site';
	import Icon from './Icon.svelte';
	import ProgressBar from './ProgressBar.svelte';
	import RuntimeBadge from './RuntimeBadge.svelte';
	import StatusPill from './StatusPill.svelte';
	import Thumb from './Thumb.svelte';

	let { site }: { site: Site } = $props();

	const href = $derived(resolve(`/sites/${site.id}`));
	const m = $derived(site.metrics_1h);
	const shown = $derived(shownStatus(site));
	const wp = $derived(branchOf(site.wp_version ?? ''));

	// The setup face: the site is not a site yet (or is on its way out).
	const setup = $derived(site.status === 'provisioning' || site.status === 'deleting');
	const job = $derived.by(() => {
		if (!setup) return null;
		const j = jobFor(site.id);
		return j && isLive(j) ? j : null;
	});
	const progress = $derived(job ? jobProgress(job, jobEvents.get(job.id) ?? []) : null);
	let now = $state(Date.now());
	$effect(() => {
		if (!job) return;
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});
	const elapsed = $derived(
		job ? formatDuration(spanMs(job.started_at ?? job.created_at, null, now)) : ''
	);

	// The card the wizard just created scrolls into view once.
	let el: HTMLElement | undefined = $state();
	$effect(() => {
		if (createSite.justCreated !== site.id || !el) return;
		el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
		createSite.justCreated = null;
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
	bind:this={el}
	class="group flex flex-col overflow-hidden frame transition-shadow duration-200 hover:shadow-plate-hover"
>
	<a {href} class="block" aria-label="Open {site.name}">
		{#if setup}
			<div
				class="flex aspect-[16/10] flex-col items-center justify-center gap-4 bg-sunken px-8 text-center"
			>
				<span class="text-md font-medium text-ink">
					{#if progress?.current}
						{progress.current.label}
					{:else if job}
						Waiting for a worker
					{:else if site.status === 'deleting'}
						Deleting
					{:else}
						Setting up
					{/if}
				</span>
				<ProgressBar
					class="w-full max-w-64"
					value={progress?.total ? progress.fraction : null}
					label="{site.status === 'deleting' ? 'Deleting' : 'Setting up'} {site.name}"
				/>
				<span class="text-xs text-ink-3 num">
					{#if progress?.total && progress.position >= 0}
						Step {Math.min(progress.position + 1, progress.total)} of {progress.total} · {elapsed}
					{:else if job}
						{elapsed}
					{:else}
						{site.status_message || 'In progress'}
					{/if}
				</span>
			</div>
		{:else}
			<Thumb {site} class="aspect-[16/10]" />
		{/if}
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

		<div class="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-ink-2">
			{#if setup}
				<span>Started {formatRelative(job?.created_at ?? site.updated_at)}</span>
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
				<span class="whitespace-nowrap num"
					><strong class="font-semibold text-ink">{formatNumber(m?.requests ?? 0)}</strong> requests this
					hour</span
				>
				<span class="whitespace-nowrap num"
					><strong class="font-semibold text-ink">{m && m.requests ? cacheRatio(m) : '–'}</strong> from
					cache</span
				>
				{#if m?.status_5xx}
					<span class="font-medium text-danger-ink num">{formatNumber(m.status_5xx)} errors</span>
				{/if}
			{/if}
			<span class="ml-auto flex shrink-0 items-center gap-2 text-ink-3">
				<RuntimeBadge runtime={site.runtime} />
				<span class="num">{wp ? `WP ${wp} · ` : ''}PHP {site.php_version}</span>
			</span>
		</div>
	</div>
</article>
