<!-- Sites list: initial GET merged with live site.status/site.deleted events. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { deletedSites, sites as liveSites } from '$lib/state/live.svelte';
	import type { Site } from '$lib/types/site';
	import { formatRelative, formatNumber, percent } from '$lib/format';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Empty from '$lib/components/Empty.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
	import Table, { td, th } from '$lib/components/Table.svelte';

	let loaded = $state<Site[] | null>(null);
	let error = $state('');

	async function load() {
		try {
			const res = await api.get<{ sites: Site[] }>('/v1/sites');
			loaded = res.sites;
			error = '';
		} catch (err) {
			error = errorText(err);
			loaded ??= [];
		}
	}
	$effect(() => {
		load();
	});

	// Live map wins over the snapshot; sites created after load appear too.
	const sites = $derived.by(() => {
		const byId: Record<string, Site> = {};
		for (const s of loaded ?? []) byId[s.id] = s;
		for (const s of liveSites.values()) byId[s.id] = s;
		for (const id of deletedSites.keys()) delete byId[id];
		return Object.values(byId).sort((a, b) => a.name.localeCompare(b.name));
	});

	function hitRate(s: Site): string {
		const m = s.metrics_1h;
		if (!m || !m.requests) return '–';
		return percent(m.cache_hit, m.requests);
	}
</script>

<svelte:head>
	<title>Sites · presso</title>
</svelte:head>

<div class="mb-5 flex items-center justify-between">
	<h1 class="text-xl font-semibold">Sites</h1>
	<Button variant="primary" href="/sites/new">New site</Button>
</div>

{#if error}
	<div class="mb-4"><Alert>{error}</Alert></div>
{/if}

{#if loaded === null}
	<p class="text-sm text-gray-500">Loading…</p>
{:else if sites.length === 0}
	<Empty title="No sites yet" text="Create your first WordPress site to get started.">
		<Button variant="primary" href="/sites/new">New site</Button>
	</Empty>
{:else}
	<Table>
		<thead class="bg-gray-50">
			<tr>
				<th class={th}>Name</th>
				<th class={th}>Domain</th>
				<th class={th}>Status</th>
				<th class={th}>PHP</th>
				<th class="{th} text-right">Requests (1h)</th>
				<th class="{th} text-right">Cache hit</th>
				<th class={th}>Created</th>
			</tr>
		</thead>
		<tbody class="divide-y divide-gray-100">
			{#each sites as s (s.id)}
				<tr class="hover:bg-gray-50">
					<td class={td}>
						<a href={resolve(`/sites/${s.id}`)} class="font-medium text-gray-900 hover:underline"
							>{s.name}</a
						>
						<span class="ml-2 text-xs text-gray-400">{s.slug}</span>
					</td>
					<td class={td}>
						<!-- eslint-disable svelte/no-navigation-without-resolve -- external site URL -->
						<a
							href={s.url}
							target="_blank"
							rel="noreferrer noopener"
							class="text-gray-700 hover:underline">{s.primary_domain} ↗</a
						>
						<!-- eslint-enable svelte/no-navigation-without-resolve -->
					</td>
					<td class={td}>
						<div class="flex items-center gap-1.5">
							<StatusPill status={s.status} />
							{#if s.status === 'running' && s.health !== 'none'}
								<StatusPill status={s.health} dot={false} />
							{/if}
						</div>
					</td>
					<td class={td}>{s.php_version}</td>
					<td class="{td} text-right tabular-nums">{formatNumber(s.metrics_1h?.requests ?? 0)}</td>
					<td class="{td} text-right tabular-nums">{hitRate(s)}</td>
					<td class="{td} text-gray-500" title={s.created_at}>{formatRelative(s.created_at)}</td>
				</tr>
			{/each}
		</tbody>
	</Table>
{/if}
