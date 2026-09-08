<!-- Activity tab: recent audit entries for this site, newest first. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { formatDateTime, formatRelative } from '$lib/format';
	import type { Activity, ActivityResponse } from '$lib/types/activity';
	import Alert from '$lib/components/Alert.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Empty from '$lib/components/Empty.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';

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
				<Skeleton class="h-3 w-16" />
				<Skeleton class="h-3 w-1/2" />
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
				<Badge tone="neutral" class="font-mono font-medium">{a.kind}</Badge>
			</li>
		{/each}
	</ol>
{/if}
