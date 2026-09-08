<!-- Job detail: what ran, on which site, and every step, seeded from the API
     and updated live. -->
<script lang="ts">
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobs, seedJob, seedJobEvents, sites } from '$lib/state/live.svelte';
	import { session } from '$lib/state/session.svelte';
	import type { Site } from '$lib/types/site';
	import { formatDateTime, formatDuration, formatRelative, spanMs } from '$lib/format';
	import type { Job, JobDetailResponse } from '$lib/types/job';
	import Alert from '$lib/components/Alert.svelte';
	import JobPanel from '$lib/components/JobPanel.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';

	const id = $derived(page.params.id ?? '');

	let loaded = $state<Job | null>(null);
	let error = $state('');

	$effect(() => {
		const jobId = id;
		loaded = null;
		if (!jobId) return;
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
	// The live map knows the site once any event about it arrived; until then
	// fetch it once so the title carries a name rather than an id.
	let fetchedSite = $state<Site | null>(null);
	$effect(() => {
		const siteId = job?.site_id;
		if (!siteId || sites.has(siteId) || fetchedSite?.id === siteId) return;
		api
			.get<{ site: Site }>(`/v1/sites/${siteId}`)
			.then((r) => (fetchedSite = r.site))
			.catch(() => {});
	});
	const site = $derived(
		job?.site_id
			? (sites.get(job.site_id) ?? (fetchedSite?.id === job.site_id ? fetchedSite : undefined))
			: undefined
	);
	const requester = $derived.by(() => {
		if (!job?.created_by) return 'the engine';
		if (job.created_by === session.user?.id) return 'you';
		return 'another operator';
	});
	const argEntries = $derived(Object.entries(job?.args ?? {}));

	let now = $state(Date.now());
	$effect(() => {
		if (job?.finished_at) return;
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	/** "site.provision" reads as "Provision". */
	function verb(kind: string): string {
		const v = kind.split('.').pop() ?? kind;
		return v.charAt(0).toUpperCase() + v.slice(1);
	}

	const title = $derived.by(() => {
		if (!job) return 'Job';
		const who = site?.name ?? (job.site_id ? job.site_id.slice(0, 8) : '');
		return who ? `${verb(job.kind)}, ${who}` : verb(job.kind);
	});

	const timing = $derived.by(() => {
		if (!job) return '';
		if (!job.started_at)
			return `Created ${formatRelative(job.created_at, now)}, waiting for a worker.`;
		const started = `Started ${formatRelative(job.started_at, now)}`;
		if (job.finished_at) {
			return `${started}, finished in ${formatDuration(spanMs(job.started_at, job.finished_at, now))}.`;
		}
		return `${started}, running for ${formatDuration(spanMs(job.started_at, null, now))}.`;
	});
</script>

<svelte:head>
	<title>Job {job ? verb(job.kind) : ''} · presso</title>
</svelte:head>

{#if error && !job}
	<PageHeader title="Job" back={{ href: resolve('/jobs'), label: 'Jobs' }} />
	<Alert>{error}</Alert>
{:else if !job}
	<PageHeader title="Job" subtitle="Loading." back={{ href: resolve('/jobs'), label: 'Jobs' }} />
	<div aria-busy="true"><Skeleton plate class="h-40" /></div>
{:else}
	<PageHeader {title} subtitle={timing} back={{ href: resolve('/jobs'), label: 'Jobs' }}>
		<div class="flex flex-wrap items-center gap-3">
			<StatusPill status={job.status} soft />
			{#if job.site_id}
				<a href={resolve(`/sites/${job.site_id}`)} class="text-xs text-ink-2 hover:text-ink">
					Open site
				</a>
			{/if}
			<span class="text-xs text-ink-3" title={formatDateTime(job.created_at)}>
				Requested by {requester}
			</span>
		</div>
	</PageHeader>

	<div class="flex flex-col gap-5">
		{#if argEntries.length}
			<Panel title="Parameters" padded={false}>
				<dl class="grid grid-cols-1 sm:grid-cols-2">
					{#each argEntries as [k, v] (k)}
						<div class="flex flex-col gap-0.5 border-b border-rule px-6 py-3.5 last:border-b-0">
							<dt class="text-xs text-ink-2">{k}</dt>
							<dd class="truncate font-mono text-xs text-ink">
								{typeof v === 'string' ? v : JSON.stringify(v)}
							</dd>
						</div>
					{/each}
				</dl>
			</Panel>
		{/if}

		<Panel title="Steps">
			<JobPanel {job} showLink={false} />
		</Panel>
	</div>
{/if}
