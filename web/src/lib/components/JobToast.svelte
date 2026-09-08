<!-- Follows the job working on a site without taking the page over: a small
     plate at the bottom right with the current step and a bar; clicking it
     opens the full timeline in a Sheet. It appears when a job is under way
     or finishes while the page is open, hides itself a few seconds after a
     success, and stays on a failure until dismissed. `open` is bindable so
     the page can raise the Sheet from elsewhere ("Show what happened"). -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { jobEvents, jobFor, loadJob } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runSiteAction } from '$lib/site-actions';
	import { isLive, jobProgress, jobTitle, jobVerb } from '$lib/job-progress';
	import { formatDuration, formatTime, spanMs } from '$lib/format';
	import type { Site } from '$lib/types/site';
	import Button from './Button.svelte';
	import Icon from './Icon.svelte';
	import JobTimeline from './JobTimeline.svelte';
	import Lamp from './Lamp.svelte';
	import ProgressBar from './ProgressBar.svelte';
	import Sheet from './Sheet.svelte';
	import StatusPill from './StatusPill.svelte';

	let { site, open = $bindable(false) }: { site: Site; open?: boolean } = $props();

	const job = $derived(jobFor(site.id));
	const progress = $derived(job ? jobProgress(job, jobEvents.get(job.id) ?? []) : null);

	// The event log of a job that started before this page was opened.
	$effect(() => {
		const id = job?.id;
		if (id && !jobEvents.has(id)) loadJob(id).catch(() => {});
	});

	// Follow a job from the moment it is seen live; a finished job that was
	// never live here (a reload after a failure) does not pop the toast.
	let followed = $state<string | null>(null);
	let dismissed = $state<string | null>(null);
	$effect(() => {
		if (job && isLive(job)) followed = job.id;
	});
	// Successes fade out on their own.
	$effect(() => {
		if (!job || job.id !== followed || job.status !== 'succeeded') return;
		const t = setTimeout(() => (dismissed = job.id), 6000);
		return () => clearTimeout(t);
	});
	const shown = $derived(job !== null && job.id === followed && job.id !== dismissed);

	let now = $state(Date.now());
	$effect(() => {
		if (!shown || job?.finished_at) return;
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});
	const elapsed = $derived(
		job ? formatDuration(spanMs(job.started_at ?? job.created_at, job.finished_at, now)) : ''
	);

	const tone = $derived(
		job?.status === 'succeeded' ? 'ok' : job?.status === 'failed' ? 'danger' : 'busy'
	);
	const stepLine = $derived.by(() => {
		if (!job || !progress) return '';
		if (job.status === 'succeeded') return `Done in ${elapsed}`;
		if (job.status === 'failed') return job.error || 'Something went wrong';
		if (!progress.current) return 'Waiting for a worker';
		return progress.current.label;
	});
	const counter = $derived(
		progress && progress.total && progress.position >= 0
			? `${Math.min(progress.position + 1, progress.total)} of ${progress.total}`
			: ''
	);

	const canRetry = $derived(
		!!job &&
			job.status === 'failed' &&
			jobVerb(job.kind) === 'provision' &&
			site.status === 'error' &&
			isAdmin()
	);
	let retrying = $state(false);
	async function retry() {
		retrying = true;
		try {
			await runSiteAction(site, 'retry');
			open = false;
		} catch {
			// The new job's failure shows up in the timeline.
		} finally {
			retrying = false;
		}
	}

	const subtitle = $derived.by(() => {
		if (!job) return '';
		const started = job.started_at ? `Started ${formatTime(job.started_at)}` : 'Queued';
		if (job.finished_at) return `${started}, took ${elapsed}.`;
		return `${started}, ${elapsed} so far.`;
	});
</script>

{#if job && shown}
	<div
		class="fixed inset-x-4 bottom-4 z-30 sm:inset-x-auto sm:right-6 sm:bottom-6 sm:w-[380px]"
		role="status"
		aria-live="polite"
	>
		<div
			class="relative overflow-hidden rounded-plate bg-surface shadow-pop transition-shadow duration-200 hover:shadow-dialog"
		>
			<button
				type="button"
				class="flex w-full flex-col gap-2.5 px-5 pt-4 pb-4 text-left"
				onclick={() => (open = true)}
				aria-label="Show the steps of this operation"
			>
				<span class="flex items-center gap-2.5 pr-8">
					<Lamp status={job.status} />
					<span class="truncate text-sm font-semibold text-ink">{jobTitle(job, site.name)}</span>
					{#if job.status !== 'succeeded'}
						<span class="ml-auto shrink-0 text-xs text-ink-3 num">{elapsed}</span>
					{/if}
				</span>
				<span
					class="line-clamp-2 text-sm leading-5 {job.status === 'failed'
						? 'text-danger-ink'
						: 'text-ink-2'}"
				>
					{stepLine}
				</span>
				<span class="flex items-center gap-3">
					<ProgressBar
						class="flex-1"
						size="sm"
						{tone}
						value={progress && (progress.total || !progress.live) ? progress.fraction : null}
					/>
					{#if counter && job.status !== 'succeeded'}
						<span class="shrink-0 text-2xs text-ink-3 num">{counter}</span>
					{:else}
						<Icon name="chevron-right" size={14} class="shrink-0 text-ink-3" />
					{/if}
				</span>
			</button>
			<Button
				variant="ghost"
				size="sm"
				icon="close"
				class="absolute top-2.5 right-2.5"
				aria-label="Hide"
				onclick={() => (dismissed = job.id)}
			/>
		</div>
	</div>
{/if}

{#if job}
	<Sheet bind:open title={jobTitle(job, site.name)} {subtitle}>
		{#snippet aside()}
			<StatusPill status={job.status} soft />
		{/snippet}
		<JobTimeline {job} />
		{#snippet footer()}
			<Button size="sm" href={resolve(`/jobs/${job.id}`)}>Open job</Button>
			{#if canRetry}
				<Button size="sm" variant="primary" icon="refresh" onclick={retry} busy={retrying}>
					Retry provisioning
				</Button>
			{/if}
		{/snippet}
	</Sheet>
{/if}
