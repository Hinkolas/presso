<!-- Metrics tab: request, latency, error and container charts for a range,
     refreshed every minute while shown. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import type { MetricsRange, MetricsResponse } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Card from '$lib/components/Card.svelte';
	import LineChart from '$lib/components/LineChart.svelte';

	let { siteId }: { siteId: string } = $props();

	const ranges: MetricsRange[] = ['1h', '24h', '7d'];
	let range = $state<MetricsRange>('1h');
	let data = $state<MetricsResponse | null>(null);
	let error = $state('');
	let loading = $state(false);

	async function load(id: string, r: MetricsRange) {
		loading = true;
		try {
			const res = await api.get<MetricsResponse>(`/v1/sites/${id}/metrics?range=${r}`);
			// Ignore a stale response after the user switched ranges.
			if (r === range) data = res;
			error = '';
		} catch (err) {
			error = errorText(err);
		} finally {
			loading = false;
		}
	}

	$effect(() => {
		const id = siteId;
		const r = range;
		load(id, r);
		const t = setInterval(() => load(id, r), 60_000);
		return () => clearInterval(t);
	});

	const ts = (b: string) => Math.floor(new Date(b).getTime() / 1000);
	const perMin = $derived((data?.step_seconds ?? 60) / 60);

	const reqTimes = $derived((data?.requests ?? []).map((b) => ts(b.bucket)));
	const reqSeries = $derived([
		(data?.requests ?? []).map((b) => b.cache_hit / perMin),
		(data?.requests ?? []).map((b) => b.cache_miss / perMin),
		(data?.requests ?? []).map((b) => b.cache_bypass / perMin),
		(data?.requests ?? []).map((b) => b.cache_stale / perMin)
	]);
	const latSeries = $derived([
		(data?.requests ?? []).map((b) => (b.requests ? b.dur_p50_ms : null)),
		(data?.requests ?? []).map((b) => (b.requests ? b.dur_p95_ms : null))
	]);
	const errSeries = $derived([
		(data?.requests ?? []).map((b) => b.status_4xx),
		(data?.requests ?? []).map((b) => b.status_5xx)
	]);
	const cTimes = $derived((data?.container ?? []).map((b) => ts(b.bucket)));
	const cpuSeries = $derived([(data?.container ?? []).map((b) => b.cpu_pct)]);
	const MB = 1024 * 1024;
	const memSeries = $derived([
		(data?.container ?? []).map((b) => b.mem_bytes / MB),
		(data?.container ?? []).map((b) => (b.mem_limit ? b.mem_limit / MB : null))
	]);
	const memMax = $derived(
		Math.max(0, ...(data?.container ?? []).map((b) => (b.mem_limit || 0) / MB))
	);
	const hasRequests = $derived((data?.requests.length ?? 0) > 0);
	const hasContainer = $derived((data?.container.length ?? 0) > 0);
</script>

<div class="flex flex-col gap-4">
	<div class="flex items-center gap-2">
		<div class="inline-flex rounded-md border border-gray-300 bg-white p-0.5 text-sm">
			{#each ranges as r (r)}
				<button
					type="button"
					onclick={() => (range = r)}
					class="rounded px-3 py-1 font-medium {range === r
						? 'bg-gray-900 text-white'
						: 'text-gray-600 hover:bg-gray-100'}"
				>
					{r}
				</button>
			{/each}
		</div>
		{#if loading}
			<span class="text-xs text-gray-400">Refreshing…</span>
		{/if}
	</div>

	{#if error}
		<Alert>{error}</Alert>
	{/if}

	{#if data}
		<div class="grid gap-4 xl:grid-cols-2">
			<Card title="Requests per minute">
				{#if hasRequests}
					<LineChart
						times={reqTimes}
						values={reqSeries}
						series={[
							{ label: 'hit', color: '#16a34a', fill: true },
							{ label: 'miss', color: '#2563eb' },
							{ label: 'bypass', color: '#6b7280' },
							{ label: 'stale', color: '#d97706' }
						]}
						format={(v) => v.toFixed(1)}
					/>
				{:else}
					<p class="text-sm text-gray-500">No requests in this range.</p>
				{/if}
			</Card>
			<Card title="Latency">
				{#if hasRequests}
					<LineChart
						times={reqTimes}
						values={latSeries}
						series={[
							{ label: 'p50', color: '#2563eb' },
							{ label: 'p95', color: '#7c3aed' }
						]}
						format={(v) => `${Math.round(v)} ms`}
					/>
				{:else}
					<p class="text-sm text-gray-500">No requests in this range.</p>
				{/if}
			</Card>
			<Card title="Errors">
				{#if hasRequests}
					<LineChart
						times={reqTimes}
						values={errSeries}
						series={[
							{ label: '4xx', color: '#d97706' },
							{ label: '5xx', color: '#dc2626' }
						]}
						format={(v) => String(Math.round(v))}
					/>
				{:else}
					<p class="text-sm text-gray-500">No requests in this range.</p>
				{/if}
			</Card>
			<Card title="CPU">
				{#if hasContainer}
					<LineChart
						times={cTimes}
						values={cpuSeries}
						series={[{ label: 'cpu', color: '#0891b2', fill: true }]}
						format={(v) => `${v.toFixed(1)}%`}
					/>
				{:else}
					<p class="text-sm text-gray-500">No container samples in this range.</p>
				{/if}
			</Card>
			<Card title="Memory">
				{#if hasContainer}
					<LineChart
						times={cTimes}
						values={memSeries}
						series={[
							{ label: 'used', color: '#4f46e5', fill: true },
							{ label: 'limit', color: '#9ca3af', dash: [4, 4] }
						]}
						format={(v) => `${Math.round(v)} MB`}
						max={memMax}
					/>
				{:else}
					<p class="text-sm text-gray-500">No container samples in this range.</p>
				{/if}
			</Card>
		</div>
	{:else if !error}
		<p class="text-sm text-gray-500">Loading…</p>
	{/if}
</div>
