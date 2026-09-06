<!-- Live view of one job: status header plus its event log, merged from the
     seeded events and the /v1/events stream. Step lines are emphasized. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import StatusPill from './StatusPill.svelte';
	import { jobEvents, jobs } from '$lib/state/live.svelte';
	import { formatDuration, formatTime, spanMs } from '$lib/format';
	import type { Job } from '$lib/types/job';

	let {
		job: initial,
		compact = false,
		showLink = true
	}: { job: Job; compact?: boolean; showLink?: boolean } = $props();

	const job = $derived(jobs.get(initial.id) ?? initial);
	const events = $derived(jobEvents.get(initial.id) ?? []);

	let now = $state(Date.now());
	$effect(() => {
		if (job.finished_at) return;
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});
	const duration = $derived(spanMs(job.started_at ?? job.created_at, job.finished_at, now));

	let scroller: HTMLDivElement | undefined = $state();
	$effect(() => {
		void events.length;
		if (scroller) scroller.scrollTop = scroller.scrollHeight;
	});

	const levelCls: Record<string, string> = {
		step: 'text-gray-900 font-semibold',
		info: 'text-gray-600',
		warn: 'text-amber-700',
		error: 'text-red-700'
	};
</script>

<div class="flex flex-col gap-2">
	<div class="flex flex-wrap items-center gap-2 text-sm">
		<StatusPill status={job.status} />
		<span class="font-medium text-gray-900">{job.kind}</span>
		<span class="text-gray-500">{formatDuration(duration)}</span>
		{#if showLink}
			<a
				href={resolve(`/jobs/${job.id}`)}
				class="ml-auto text-xs text-gray-500 underline hover:text-gray-900"
			>
				Job details
			</a>
		{/if}
	</div>
	{#if job.error}
		<p class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-800">{job.error}</p>
	{/if}
	<div
		bind:this={scroller}
		class="overflow-y-auto rounded-md border border-gray-200 bg-gray-50 p-3 font-mono text-xs {compact
			? 'max-h-48'
			: 'max-h-[32rem]'}"
	>
		{#if events.length === 0}
			<p class="text-gray-500">
				{job.status === 'queued' ? 'Waiting to start…' : 'No events yet.'}
			</p>
		{:else}
			{#each events as ev (ev.id)}
				<div class="flex gap-3 {levelCls[ev.level] ?? 'text-gray-600'}">
					<span class="shrink-0 text-gray-400">{formatTime(ev.ts)}</span>
					{#if ev.level === 'step'}
						<span class="shrink-0">▸ {ev.step}</span>
					{:else if ev.step}
						<span class="shrink-0 text-gray-400">{ev.step}</span>
					{/if}
					<span class="break-all whitespace-pre-wrap">{ev.message}</span>
				</div>
			{/each}
		{/if}
	</div>
</div>
