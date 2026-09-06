<!-- Activity tab: recent audit entries for this site. -->
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
	<p class="text-sm text-gray-500">Loading…</p>
{:else if items.length === 0}
	<Empty title="No activity yet" />
{:else}
	<ol class="divide-y divide-gray-100 rounded-lg border border-gray-200 bg-white">
		{#each items as a (a.id)}
			<li class="flex items-start gap-4 px-4 py-3 text-sm">
				<span class="w-28 shrink-0 text-xs text-gray-500" title={formatDateTime(a.ts)}>
					{formatRelative(a.ts)}
				</span>
				<span class="w-32 shrink-0 font-mono text-xs text-gray-600">{a.kind}</span>
				<span class="min-w-0 flex-1 text-gray-800">{a.message}</span>
			</li>
		{/each}
	</ol>
{/if}
