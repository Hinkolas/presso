<!-- Jobs list: recent jobs from the API merged with live job.updated events. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobs as liveJobs, sites } from '$lib/state/live.svelte';
	import { formatDuration, formatRelative, spanMs } from '$lib/format';
	import type { Job, JobsResponse } from '$lib/types/job';
	import type { Site } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Empty from '$lib/components/Empty.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
	import Table, { td, th } from '$lib/components/Table.svelte';

	let loaded = $state<Job[] | null>(null);
	let siteNames = $state<Record<string, Site>>({});
	let error = $state('');

	$effect(() => {
		api
			.get<JobsResponse>('/v1/jobs?limit=50')
			.then((r) => (loaded = r.jobs))
			.catch((err) => {
				error = errorText(err);
				loaded = [];
			});
		api
			.get<{ sites: Site[] }>('/v1/sites')
			.then((r) => (siteNames = Object.fromEntries(r.sites.map((s) => [s.id, s]))))
			.catch(() => {});
	});

	const jobs = $derived.by(() => {
		const byId: Record<string, Job> = {};
		for (const j of loaded ?? []) byId[j.id] = j;
		for (const j of liveJobs.values()) {
			const cur = byId[j.id];
			if (!cur || cur.updated_at <= j.updated_at) byId[j.id] = j;
		}
		return Object.values(byId)
			.sort((a, b) => b.created_at.localeCompare(a.created_at))
			.slice(0, 100);
	});

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	function siteOf(j: Job): Site | undefined {
		if (!j.site_id) return undefined;
		return sites.get(j.site_id) ?? siteNames[j.site_id];
	}
</script>

<svelte:head>
	<title>Jobs · presso</title>
</svelte:head>

<h1 class="mb-5 text-xl font-semibold">Jobs</h1>

{#if error}
	<div class="mb-4"><Alert>{error}</Alert></div>
{/if}

{#if loaded === null}
	<p class="text-sm text-gray-500">Loading…</p>
{:else if jobs.length === 0}
	<Empty title="No jobs yet" text="Site operations show up here as they run." />
{:else}
	<Table>
		<thead class="bg-gray-50">
			<tr>
				<th class={th}>Kind</th>
				<th class={th}>Site</th>
				<th class={th}>Status</th>
				<th class={th}>Created</th>
				<th class="{th} text-right">Duration</th>
			</tr>
		</thead>
		<tbody class="divide-y divide-gray-100">
			{#each jobs as j (j.id)}
				{@const s = siteOf(j)}
				<tr class="hover:bg-gray-50">
					<td class={td}>
						<a href={resolve(`/jobs/${j.id}`)} class="font-medium text-gray-900 hover:underline"
							>{j.kind}</a
						>
					</td>
					<td class={td}>
						{#if s}
							<a href={resolve(`/sites/${s.id}`)} class="hover:underline">{s.name}</a>
						{:else if j.site_id}
							<span class="font-mono text-xs text-gray-500">{j.site_id.slice(0, 8)}</span>
						{:else}
							<span class="text-gray-400">–</span>
						{/if}
					</td>
					<td class={td}>
						<StatusPill status={j.status} />
						{#if j.error}
							<span class="ml-2 text-xs text-red-700">{j.error}</span>
						{/if}
					</td>
					<td class="{td} text-gray-500" title={j.created_at}
						>{formatRelative(j.created_at, now)}</td
					>
					<td class="{td} text-right text-gray-600 tabular-nums">
						{formatDuration(spanMs(j.started_at, j.finished_at, now))}
					</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}
