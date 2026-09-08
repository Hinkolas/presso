<!-- Activity tab on System: recent operations from the API merged with live
     job.updated events. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobs as liveJobs, sites } from '$lib/state/live.svelte';
	import { formatDateTime, formatDuration, formatRelative, spanMs } from '$lib/format';
	import type { Job, JobsResponse } from '$lib/types/job';
	import type { Site } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Empty from '$lib/components/Empty.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
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

	const all = $derived.by(() => {
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

	type Filter = 'all' | 'running' | 'failed';
	let filter = $state<Filter>('all');
	const jobs = $derived.by(() => {
		switch (filter) {
			case 'running':
				return all.filter((j) => j.status === 'queued' || j.status === 'running');
			case 'failed':
				return all.filter((j) => j.status === 'failed');
			default:
				return all;
		}
	});

	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(t);
	});

	const summary = $derived.by(() => {
		if (all.length === 0) return 'Everything the engine has done to your sites.';
		const running = all.filter((j) => j.status === 'queued' || j.status === 'running').length;
		const dayAgo = now - 24 * 60 * 60 * 1000;
		const finished = all.filter(
			(j) => j.finished_at && new Date(j.finished_at).getTime() >= dayAgo
		).length;
		const failed = all.filter((j) => j.status === 'failed').length;
		const parts: string[] = [];
		if (running) parts.push(`${running} running`);
		parts.push(`${finished} finished today`);
		if (failed) parts.push(`${failed} failed`);
		return parts.join(', ');
	});

	function siteOf(j: Job): Site | undefined {
		if (!j.site_id) return undefined;
		return sites.get(j.site_id) ?? siteNames[j.site_id];
	}

	/** "site.provision" reads as "Provision". */
	function verb(kind: string): string {
		const v = kind.split('.').pop() ?? kind;
		return v.charAt(0).toUpperCase() + v.slice(1);
	}
</script>

<div class="mb-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
	<p class="text-md text-ink-2">{loaded === null ? 'Loading recent operations.' : summary}</p>
	{#if all.length > 0}
		<Segmented
			bind:value={filter}
			options={[
				{ value: 'all', label: 'All' },
				{ value: 'running', label: 'Running' },
				{ value: 'failed', label: 'Failed' }
			]}
		/>
	{/if}
</div>

{#if error}
	<Alert class="mb-5">{error}</Alert>
{/if}

{#if loaded === null}
	<Table>
		<tbody aria-busy="true">
			{#each [0, 1, 2, 3] as i (i)}
				<tr>
					<td class={td}><Skeleton class="h-4 w-40" /></td>
					<td class={td}><Skeleton class="h-7 w-24 rounded-full" /></td>
					<td class={td}><Skeleton class="h-4 w-24" /></td>
					<td class={td}><Skeleton class="ml-auto h-4 w-12" /></td>
					<td class={td}></td>
				</tr>
			{/each}
		</tbody>
	</Table>
{:else if all.length === 0}
	<Empty
		title="Nothing has run yet"
		text="Creating, starting or changing a site shows up here as it happens."
	/>
{:else if jobs.length === 0}
	<Empty
		title={filter === 'running' ? 'Nothing is running' : 'No failed jobs'}
		text={filter === 'running'
			? 'The engine is idle.'
			: 'Every recent operation finished successfully.'}
	/>
{:else}
	<Table>
		<thead>
			<tr>
				<th class={th}>Operation</th>
				<th class={th}>Status</th>
				<th class={th}>Started</th>
				<th class="{th} text-right">Duration</th>
				<th class={th}><span class="sr-only">Open</span></th>
			</tr>
		</thead>
		<tbody>
			{#each jobs as j (j.id)}
				{@const s = siteOf(j)}
				<tr class="transition-colors hover:bg-sunken/60">
					<td class={td}>
						<a
							href={resolve(`/jobs/${j.id}`)}
							class="font-medium text-ink hover:text-brand"
							title={j.kind}>{verb(j.kind)}</a
						>
						<div class="mt-0.5 text-xs text-ink-2">
							{#if s}
								<a href={resolve(`/sites/${s.id}`)} class="hover:text-ink">{s.name}</a>
							{:else if j.site_id}
								<span class="font-mono">{j.site_id.slice(0, 8)}</span>
							{:else}
								<span class="text-ink-3">No site</span>
							{/if}
						</div>
					</td>
					<td class={td}>
						<div class="flex flex-col items-start gap-1.5">
							<StatusPill status={j.status} soft />
							{#if j.error}
								<p class="max-w-xs truncate text-xs text-danger-ink" title={j.error}>
									{j.error}
								</p>
							{/if}
						</div>
					</td>
					<td class="{td} text-ink-2 num" title={formatDateTime(j.started_at ?? j.created_at)}>
						{formatRelative(j.started_at ?? j.created_at, now)}
					</td>
					<td class="{td} text-right text-ink-2 num">
						{formatDuration(spanMs(j.started_at, j.finished_at, now))}
					</td>
					<td class="{td} w-16 pr-4 text-right">
						<Button
							variant="ghost"
							size="sm"
							icon="chevron-right"
							href={resolve(`/jobs/${j.id}`)}
							aria-label="Open job"
						/>
					</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}
