<!-- Metrics tab: request, latency, error and container charts for a range,
     refreshed every minute while shown. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { formatDuration, formatNumber, percent } from '$lib/format';
	import type { MetricsRange, MetricsResponse } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import LineChart from '$lib/components/LineChart.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Stat from '$lib/components/Stat.svelte';

	let { siteId }: { siteId: string } = $props();

	const ranges: { value: MetricsRange; label: string }[] = [
		{ value: '1h', label: 'Last hour' },
		{ value: '24h', label: '24 hours' },
		{ value: '7d', label: '7 days' }
	];
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
	const spans: Record<MetricsRange, number> = { '1h': 3600, '24h': 86400, '7d': 7 * 86400 };
	const window = $derived.by<[number, number]>(() => {
		const end = Math.floor(Date.now() / 1000);
		void data;
		return [end - spans[range], end];
	});
	const perMin = $derived((data?.step_seconds ?? 60) / 60);
	const req = $derived(data?.requests ?? []);
	const cont = $derived(data?.container ?? []);

	const reqTimes = $derived(req.map((b) => ts(b.bucket)));
	const reqSeries = $derived([
		req.map((b) => b.cache_hit / perMin),
		req.map((b) => b.cache_miss / perMin),
		req.map((b) => b.cache_bypass / perMin),
		req.map((b) => b.cache_stale / perMin)
	]);
	const latSeries = $derived([
		req.map((b) => (b.requests ? b.dur_p50_ms : null)),
		req.map((b) => (b.requests ? b.dur_p95_ms : null))
	]);
	const errSeries = $derived([req.map((b) => b.status_4xx), req.map((b) => b.status_5xx)]);
	const cTimes = $derived(cont.map((b) => ts(b.bucket)));
	const cpuSeries = $derived([cont.map((b) => b.cpu_pct)]);
	const MB = 1024 * 1024;
	const memSeries = $derived([
		cont.map((b) => b.mem_bytes / MB),
		cont.map((b) => (b.mem_limit ? b.mem_limit / MB : null))
	]);
	const memMax = $derived(Math.max(0, ...cont.map((b) => (b.mem_limit || 0) / MB)));
	const hasRequests = $derived(req.length > 0);
	const hasContainer = $derived(cont.length > 0);

	// Totals for the strip above the charts.
	const totals = $derived.by(() => {
		let requests = 0;
		let cached = 0;
		let errors = 0;
		let p95 = 0;
		for (const b of req) {
			requests += b.requests;
			cached += b.cache_hit + b.cache_stale;
			errors += b.status_5xx;
			if (b.requests && b.dur_p95_ms > p95) p95 = b.dur_p95_ms;
		}
		return { requests, cached, errors, p95 };
	});
</script>

<div class="flex flex-col gap-5">
	<div class="flex flex-wrap items-center gap-3">
		<Segmented bind:value={range} options={ranges} />
		{#if loading}
			<span class="text-xs text-ink-3">Refreshing</span>
		{/if}
	</div>

	{#if error}
		<Alert>{error}</Alert>
	{/if}

	{#if data}
		<div class="grid grid-cols-2 gap-x-6 gap-y-5 frame px-6 py-5 sm:grid-cols-4">
			<Stat label="Requests" value={formatNumber(totals.requests)} />
			<Stat
				label="Served from cache"
				value={totals.requests ? percent(totals.cached, totals.requests) : '–'}
				tone={totals.requests ? 'ok' : 'ink'}
			/>
			<Stat label="Slowest p95" value={totals.p95 ? formatDuration(totals.p95) : '–'} />
			<Stat
				label="Server errors"
				value={formatNumber(totals.errors)}
				tone={totals.errors ? 'danger' : 'ink'}
			/>
		</div>

		<div class="grid gap-5 xl:grid-cols-2">
			{#snippet placeholder(text: string)}
				<div
					class="grid h-40 place-items-center rounded-plate border border-dashed border-rule-strong text-sm text-ink-3"
				>
					{text}
				</div>
			{/snippet}

			<Panel title="Requests per minute">
				{#if hasRequests}
					<LineChart
						xRange={window}
						times={reqTimes}
						values={reqSeries}
						series={[
							{ label: 'hit', tone: 'ok', fill: true },
							{ label: 'miss', tone: 'info' },
							{ label: 'bypass', tone: 'ink-3' },
							{ label: 'stale', tone: 'busy' }
						]}
						format={(v) => (v >= 10 ? String(Math.round(v)) : v.toFixed(1))}
					/>
				{:else}
					{@render placeholder('No requests in this range.')}
				{/if}
			</Panel>
			<Panel title="Response time">
				{#if hasRequests}
					<LineChart
						xRange={window}
						times={reqTimes}
						values={latSeries}
						series={[
							{ label: 'p50', tone: 'info' },
							{ label: 'p95', tone: 'brand' }
						]}
						format={(v) => `${Math.round(v)} ms`}
					/>
				{:else}
					{@render placeholder('No requests in this range.')}
				{/if}
			</Panel>
			<Panel title="Errors">
				{#if hasRequests}
					<LineChart
						xRange={window}
						times={reqTimes}
						values={errSeries}
						series={[
							{ label: '4xx', tone: 'busy' },
							{ label: '5xx', tone: 'danger' }
						]}
						format={(v) => String(Math.round(v))}
					/>
				{:else}
					{@render placeholder('No requests in this range.')}
				{/if}
			</Panel>
			<Panel title="CPU">
				{#if hasContainer}
					<LineChart
						xRange={window}
						times={cTimes}
						values={cpuSeries}
						series={[{ label: 'cpu', tone: 'brand', fill: true }]}
						format={(v) => `${v.toFixed(1)}%`}
					/>
				{:else}
					{@render placeholder('No container samples in this range.')}
				{/if}
			</Panel>
			<Panel title="Memory">
				{#if hasContainer}
					<LineChart
						xRange={window}
						times={cTimes}
						values={memSeries}
						series={[
							{ label: 'used', tone: 'brand', fill: true },
							{ label: 'limit', tone: 'ink-3', dash: [4, 4] }
						]}
						format={(v) => `${Math.round(v)} MB`}
						max={memMax}
					/>
				{:else}
					{@render placeholder('No container samples in this range.')}
				{/if}
			</Panel>
		</div>
	{:else if !error}
		<div class="grid gap-5 xl:grid-cols-2" aria-busy="true">
			{#each [0, 1] as i (i)}
				<div class="h-64 animate-pulse frame bg-sunken"></div>
			{/each}
		</div>
	{/if}
</div>
