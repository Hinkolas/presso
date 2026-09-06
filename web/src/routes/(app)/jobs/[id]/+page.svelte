<!-- Job detail: header and event log, seeded from the API and updated live. -->
<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobs, seedJob, seedJobEvents, sites } from '$lib/state/live.svelte';
	import { formatDateTime } from '$lib/format';
	import type { Job, JobDetailResponse } from '$lib/types/job';
	import Alert from '$lib/components/Alert.svelte';
	import Card from '$lib/components/Card.svelte';
	import JobPanel from '$lib/components/JobPanel.svelte';

	const id = $derived(page.params.id ?? '');

	let loaded = $state<Job | null>(null);
	let error = $state('');

	$effect(() => {
		const jobId = id;
		loaded = null;
		api
			.get<JobDetailResponse>(`/v1/jobs/${jobId}`)
			.then((r) => {
				seedJob(r.job);
				seedJobEvents(jobId, r.events);
				loaded = r.job;
			})
			.catch((err) => (error = errorText(err)));
	});

	const job = $derived(jobs.get(id) ?? loaded);
	const site = $derived(job?.site_id ? sites.get(job.site_id) : undefined);
	const argEntries = $derived(Object.entries(job?.args ?? {}));
</script>

<svelte:head>
	<title>Job {job?.kind ?? ''} · presso</title>
</svelte:head>

<div class="mb-4 flex items-center gap-3">
	<a href={resolve('/jobs')} class="text-sm text-gray-500 hover:text-gray-900">← Jobs</a>
</div>

{#if error && !job}
	<Alert>{error}</Alert>
{:else if !job}
	<p class="text-sm text-gray-500">Loading…</p>
{:else}
	<header class="mb-5">
		<h1 class="text-xl font-semibold">{job.kind}</h1>
		<dl class="mt-2 flex flex-wrap gap-x-6 gap-y-1 text-sm text-gray-600">
			<div>
				<dt class="inline text-gray-400">Site:</dt>
				<dd class="inline">
					{#if job.site_id}
						<a href={resolve(`/sites/${job.site_id}`)} class="hover:underline">
							{site?.name ?? job.site_id.slice(0, 8)}
						</a>
					{:else}
						–
					{/if}
				</dd>
			</div>
			<div>
				<dt class="inline text-gray-400">Created:</dt>
				<dd class="inline">{formatDateTime(job.created_at)}</dd>
			</div>
			<div>
				<dt class="inline text-gray-400">Started:</dt>
				<dd class="inline">{formatDateTime(job.started_at)}</dd>
			</div>
			<div>
				<dt class="inline text-gray-400">Finished:</dt>
				<dd class="inline">{formatDateTime(job.finished_at)}</dd>
			</div>
			<div>
				<dt class="inline text-gray-400">By:</dt>
				<dd class="inline font-mono text-xs">{job.created_by || '–'}</dd>
			</div>
		</dl>
		{#if argEntries.length}
			<div class="mt-2 flex flex-wrap gap-1.5">
				{#each argEntries as [k, v] (k)}
					<span class="rounded bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-700">
						{k}={typeof v === 'string' ? v : JSON.stringify(v)}
					</span>
				{/each}
			</div>
		{/if}
	</header>

	<Card title="Events">
		<JobPanel {job} showLink={false} />
	</Card>
{/if}
