<!-- Live view of one job: status line, then the step log. Steps are the
     spine; info lines hang under them. Merged from seeded events and the
     /v1/events stream. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import Lamp from './Lamp.svelte';
	import Icon from './Icon.svelte';
	import { statusLabel, toneOf, toneText } from './status';
	import { jobEvents, jobs } from '$lib/state/live.svelte';
	import { formatDuration, formatTime, spanMs } from '$lib/format';
	import type { Job, JobEvent } from '$lib/types/job';

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
	const live = $derived(job.status === 'queued' || job.status === 'running');

	// Group info/warn/error lines under the step that preceded them.
	type Group = { step: JobEvent | null; lines: JobEvent[] };
	const groups = $derived.by(() => {
		const out: Group[] = [];
		for (const ev of events) {
			if (ev.level === 'step') out.push({ step: ev, lines: [] });
			else {
				if (out.length === 0) out.push({ step: null, lines: [] });
				out[out.length - 1].lines.push(ev);
			}
		}
		return out;
	});

	let scroller: HTMLDivElement | undefined = $state();
	$effect(() => {
		void events.length;
		if (scroller) scroller.scrollTop = scroller.scrollHeight;
	});

	function stepState(i: number): 'done' | 'active' | 'failed' {
		const last = i === groups.length - 1;
		if (!last) return 'done';
		if (job.status === 'failed') return 'failed';
		if (live) return 'active';
		return 'done';
	}

	/** Job kinds are "site.provision"; show the verb. */
	function kindLabel(kind: string): string {
		const verb = kind.split('.').pop() ?? kind;
		return verb.charAt(0).toUpperCase() + verb.slice(1);
	}
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
		<span class="inline-flex items-center gap-2 font-medium {toneText[toneOf(job.status)]}">
			<Lamp status={job.status} />
			{statusLabel(job.status)}
		</span>
		<span class="text-ink">{kindLabel(job.kind)}</span>
		<span class="text-ink-3 num">{formatDuration(duration)}</span>
		{#if showLink}
			<a
				href={resolve(`/jobs/${job.id}`)}
				class="ml-auto text-xs text-ink-2 underline decoration-rule-strong underline-offset-2 hover:text-ink"
			>
				Open job
			</a>
		{/if}
	</div>

	{#if job.error}
		<p class="rounded-control border border-danger/30 bg-danger-soft px-3 py-2 text-sm text-danger">
			{job.error}
		</p>
	{/if}

	<div bind:this={scroller} class="overflow-y-auto {compact ? 'max-h-56' : 'max-h-[36rem]'}">
		{#if groups.length === 0}
			<p class="py-2 text-sm text-ink-3">
				{job.status === 'queued' ? 'Waiting for a worker.' : 'No events recorded.'}
			</p>
		{:else}
			<ol class="flex flex-col">
				{#each groups as g, i (g.step?.id ?? `pre-${i}`)}
					{@const state = stepState(i)}
					<li class="relative flex gap-3 pb-3 last:pb-0">
						<!-- spine -->
						<div class="flex w-4 shrink-0 flex-col items-center">
							<span
								class="mt-1 flex size-4 items-center justify-center rounded-full border {state ===
								'failed'
									? 'border-danger bg-danger text-white'
									: state === 'active'
										? 'border-busy bg-surface'
										: 'border-ok bg-ok text-white'}"
							>
								{#if state === 'done'}
									<Icon name="check" size={10} />
								{:else if state === 'failed'}
									<Icon name="close" size={10} />
								{:else}
									<span class="size-1.5 breathe rounded-full bg-busy"></span>
								{/if}
							</span>
							{#if i < groups.length - 1}
								<span class="mt-1 w-px flex-1 bg-rule"></span>
							{/if}
						</div>
						<div class="min-w-0 flex-1">
							{#if g.step}
								<div class="flex items-baseline gap-3">
									<span class="text-sm font-medium text-ink">{g.step.message}</span>
									<span class="ml-auto shrink-0 text-2xs text-ink-3 num"
										>{formatTime(g.step.ts)}</span
									>
								</div>
							{/if}
							{#if g.lines.length}
								<ul class="mt-1 flex flex-col gap-0.5 font-mono text-xs leading-5">
									{#each g.lines as ev (ev.id)}
										<li
											class="break-all whitespace-pre-wrap {ev.level === 'error'
												? 'text-danger'
												: ev.level === 'warn'
													? 'text-busy'
													: 'text-ink-2'}"
										>
											{ev.message}
										</li>
									{/each}
								</ul>
							{/if}
						</div>
					</li>
				{/each}
			</ol>
		{/if}
	</div>
</div>
