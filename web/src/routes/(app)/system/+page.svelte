<!-- System page: docker, platform components, edge, sites by status, disk. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { live } from '$lib/state/live.svelte';
	import { formatBytes, formatDateTime, formatRelative, formatUptime } from '$lib/format';
	import type { SystemStatus } from '$lib/types/system';
	import Alert from '$lib/components/Alert.svelte';
	import Card from '$lib/components/Card.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
	import Table, { td, th } from '$lib/components/Table.svelte';

	let status = $state<SystemStatus | null>(null);
	let error = $state('');

	async function load() {
		try {
			status = await api.get<SystemStatus>('/v1/system/status');
			error = '';
		} catch (err) {
			error = errorText(err);
		}
	}

	$effect(() => {
		load();
		const t = setInterval(load, 30_000);
		return () => clearInterval(t);
	});

	const components = $derived(live.platform ?? status?.components ?? []);
	const siteCounts = $derived(Object.entries(status?.sites ?? {}).sort());
</script>

<svelte:head>
	<title>System · presso</title>
</svelte:head>

<div class="mb-5 flex items-center justify-between">
	<h1 class="text-xl font-semibold">System</h1>
	{#if status}
		<span class="text-sm text-gray-500">
			presso {status.version} · up {formatUptime(status.uptime_seconds)}
		</span>
	{/if}
</div>

{#if error}
	<div class="mb-4"><Alert>{error}</Alert></div>
{/if}

{#if !status}
	{#if !error}<p class="text-sm text-gray-500">Loading…</p>{/if}
{:else}
	<div class="grid gap-4 lg:grid-cols-2">
		<Card title="Docker">
			<div class="mb-3">
				<StatusPill
					status={status.docker.ok ? 'ok' : 'error'}
					label={status.docker.ok ? 'connected' : 'unavailable'}
				/>
			</div>
			{#if status.docker.error}
				<Alert>{status.docker.error}</Alert>
			{:else}
				<dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
					<dt class="text-gray-500">Version</dt>
					<dd class="font-medium">{status.docker.version || '–'}</dd>
					<dt class="text-gray-500">API version</dt>
					<dd class="font-medium">{status.docker.api_version || '–'}</dd>
					<dt class="text-gray-500">Host</dt>
					<dd class="font-medium">{status.docker.os || '–'} / {status.docker.arch || '–'}</dd>
				</dl>
			{/if}
		</Card>

		<Card title="Edge">
			<dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
				<dt class="text-gray-500">Last applied</dt>
				<dd class="font-medium" title={formatDateTime(status.edge.last_applied_at)}>
					{status.edge.last_applied_at ? formatRelative(status.edge.last_applied_at) : 'never'}
				</dd>
				<dt class="text-gray-500">Routed sites</dt>
				<dd class="font-medium">{status.edge.sites}</dd>
			</dl>
			{#if status.edge.last_error}
				<div class="mt-3"><Alert>{status.edge.last_error}</Alert></div>
			{/if}
		</Card>

		<div class="lg:col-span-2">
			<Table>
				<thead class="bg-gray-50">
					<tr>
						<th class={th}>Component</th>
						<th class={th}>State</th>
						<th class={th}>Health</th>
						<th class={th}>Image</th>
						<th class={th}>Error</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-100">
					{#each components as c (c.name)}
						<tr>
							<td class="{td} font-medium">{c.name}</td>
							<td class={td}><StatusPill status={c.state} /></td>
							<td class={td}>
								{#if c.health && c.health !== 'none'}
									<StatusPill status={c.health} dot={false} />
								{:else}
									<span class="text-gray-400">–</span>
								{/if}
							</td>
							<td class="{td} font-mono text-xs text-gray-600">{c.image || '–'}</td>
							<td class="{td} text-xs text-red-700">{c.error ?? ''}</td>
						</tr>
					{:else}
						<tr><td class="{td} text-gray-500" colspan="5">No components reported.</td></tr>
					{/each}
				</tbody>
			</Table>
		</div>

		<Card title="Sites by status">
			{#if siteCounts.length === 0}
				<p class="text-sm text-gray-500">No sites.</p>
			{:else}
				<ul class="flex flex-wrap gap-3">
					{#each siteCounts as [s, n] (s)}
						<li class="flex items-center gap-2 text-sm">
							<StatusPill status={s} />
							<span class="font-semibold tabular-nums">{n}</span>
						</li>
					{/each}
				</ul>
			{/if}
		</Card>

		<Card title="Disk usage">
			<dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
				<dt class="text-gray-500">Data directory</dt>
				<dd class="font-medium tabular-nums">{formatBytes(status.disk.data_dir_bytes)}</dd>
				<dt class="text-gray-500">Images</dt>
				<dd class="font-medium tabular-nums">{formatBytes(status.disk.images_bytes)}</dd>
				<dt class="text-gray-500">Containers</dt>
				<dd class="font-medium tabular-nums">{formatBytes(status.disk.containers_bytes)}</dd>
				<dt class="text-gray-500">Volumes</dt>
				<dd class="font-medium tabular-nums">{formatBytes(status.disk.volumes_bytes)}</dd>
			</dl>
		</Card>
	</div>
{/if}
