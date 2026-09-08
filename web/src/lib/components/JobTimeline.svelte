<!-- Every step of a job as a spine: done ones ticked, the running one
     breathing, the ones still to come hollow. Each step carries the
     handler's wording and any lines it logged; the failure, if any, sits
     under the step that hit it. Live from the event stream. -->
<script lang="ts">
	import Alert from './Alert.svelte';
	import Icon from './Icon.svelte';
	import { jobEvents, jobs } from '$lib/state/live.svelte';
	import { jobProgress, type ProgressStep } from '$lib/job-progress';
	import { formatDuration, formatTime, spanMs } from '$lib/format';
	import type { Job } from '$lib/types/job';

	let { job: initial, class: klass = '' }: { job: Job; class?: string } = $props();

	const job = $derived(jobs.get(initial.id) ?? initial);
	const progress = $derived(jobProgress(job, jobEvents.get(job.id) ?? []));

	let now = $state(Date.now());
	$effect(() => {
		if (job.finished_at) return;
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	function took(s: ProgressStep): string {
		if (!s.startedAt) return '';
		const ms = spanMs(s.startedAt, s.endedAt, now);
		return ms === null || ms < 1000 ? '' : formatDuration(ms);
	}
	// The runner logs the failure message as the last error line as well;
	// the alert already says it.
	const lines = (s: ProgressStep) =>
		s.lines.filter((l) => !(l.level === 'error' && l.message === job.error));
</script>

<div class="flex flex-col gap-4 {klass}">
	{#if job.error}
		<Alert>{job.error}</Alert>
	{/if}

	{#if progress.preamble.length}
		<ul class="flex flex-col gap-0.5 font-mono text-xs leading-5 text-ink-2">
			{#each progress.preamble as ev (ev.id)}
				<li class="break-all whitespace-pre-wrap">{ev.message}</li>
			{/each}
		</ul>
	{/if}

	{#if progress.steps.length === 0}
		<p class="text-sm text-ink-3">
			{job.status === 'queued' ? 'Waiting for a worker.' : 'No steps recorded.'}
		</p>
	{:else}
		<ol class="flex flex-col">
			{#each progress.steps as s, i (`${s.name}-${i}`)}
				{@const muted = s.state === 'todo' || s.state === 'skipped'}
				<li class="relative flex gap-3.5 pb-4 last:pb-0">
					<div class="flex w-5 shrink-0 flex-col items-center">
						<span
							class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full {s.state === 'done'
								? 'bg-ok text-white'
								: s.state === 'failed'
									? 'bg-danger text-white'
									: s.state === 'active'
										? 'border-[1.5px] border-busy bg-surface'
										: 'border-[1.5px] border-dashed border-rule-strong bg-surface'}"
						>
							{#if s.state === 'done'}
								<Icon name="check" size={12} />
							{:else if s.state === 'failed'}
								<Icon name="close" size={12} />
							{:else if s.state === 'active'}
								<span class="size-2 breathe rounded-full bg-busy"></span>
							{:else if s.state === 'skipped'}
								<span class="h-px w-2 bg-rule-strong"></span>
							{/if}
						</span>
						{#if i < progress.steps.length - 1}
							<span
								class="mt-1.5 w-px flex-1 {s.state === 'done' || s.state === 'failed'
									? 'bg-rule-strong'
									: 'bg-rule'}"
							></span>
						{/if}
					</div>
					<div class="min-w-0 flex-1">
						<div class="flex items-baseline gap-3">
							<span
								class="text-sm font-medium {s.state === 'failed'
									? 'text-danger-ink'
									: muted
										? 'text-ink-3'
										: 'text-ink'}"
							>
								{s.label}
							</span>
							<span class="ml-auto shrink-0 text-2xs text-ink-3 num">
								{#if s.state === 'skipped'}
									Skipped
								{:else if s.state === 'todo'}
									{job.status === 'failed' ? 'Not reached' : ''}
								{:else if s.startedAt}
									<span title={formatTime(s.startedAt)}>{took(s)}</span>
								{/if}
							</span>
						</div>
						{#if s.message && s.message !== s.label}
							<p class="mt-0.5 text-xs leading-5 text-ink-2">{s.message}</p>
						{/if}
						{#if lines(s).length}
							<ul class="mt-1.5 flex flex-col gap-0.5 font-mono text-xs leading-5">
								{#each lines(s) as ev (ev.id)}
									<li
										class="break-all whitespace-pre-wrap {ev.level === 'error'
											? 'text-danger-ink'
											: ev.level === 'warn'
												? 'text-busy-ink'
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
