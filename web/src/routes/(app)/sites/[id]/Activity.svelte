<!-- Activity tab: recent audit entries for this site, newest first. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { formatDateTime, formatRelative } from '$lib/format';
	import type { Activity, ActivityResponse } from '$lib/types/activity';
	import Alert from '$lib/components/Alert.svelte';
	import Empty from '$lib/components/Empty.svelte';

	let { siteId }: { siteId: string } = $props();

	let items = $state<Activity[] | null>(null);
	let error = $state('');

	$effect(() => {
		const id = siteId;
		items = null;
		api
			.get<ActivityResponse>(`/v1/sites/${id}/activity?limit=50`)
			.then((r) => (items = r.activity))
			.catch((err) => {
				error = errorText(err);
				items = [];
			});
	});
</script>

{#if error}
	<Alert>{error}</Alert>
{:else if items === null}
	<div class="flex flex-col frame" aria-busy="true">
		{#each [0, 1, 2, 3] as i (i)}
			<div class="flex items-center gap-4 border-b border-rule px-6 py-4 last:border-b-0">
				<div class="h-3 w-16 animate-pulse rounded bg-sunken"></div>
				<div class="h-3 w-1/2 animate-pulse rounded bg-sunken"></div>
			</div>
		{/each}
	</div>
{:else if items.length === 0}
	<Empty title="Nothing recorded yet" text="Operations on this site show up here as they happen." />
{:else}
	<ol class="flex flex-col frame">
		{#each items as a (a.id)}
			<li class="flex items-start gap-4 border-b border-rule px-6 py-4 last:border-b-0">
				<span class="w-24 shrink-0 pt-px text-xs text-ink-3 num" title={formatDateTime(a.ts)}>
					{formatRelative(a.ts)}
				</span>
				<span class="min-w-0 flex-1 text-sm leading-5 text-ink">{a.message}</span>
				<span class="shrink-0 rounded bg-sunken px-1.5 py-0.5 font-mono text-2xs text-ink-2">
					{a.kind}
				</span>
			</li>
		{/each}
	</ol>
{/if}
