<!-- uPlot wrapper: time on the x axis, one or more numeric series. Resizes with
     its container and rebuilds when the series definitions change. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import uPlot from 'uplot';
	import 'uplot/dist/uPlot.min.css';

	export type Series = { label: string; color: string; dash?: number[]; fill?: boolean };

	let {
		times,
		series,
		values,
		height = 220,
		unit = '',
		format,
		max
	}: {
		/** Unix seconds, ascending. */
		times: number[];
		series: Series[];
		/** One array per series, aligned with `times`; null for gaps. */
		values: (number | null)[][];
		height?: number;
		unit?: string;
		format?: (v: number) => string;
		max?: number;
	} = $props();

	let host: HTMLDivElement | undefined = $state();
	let plot: uPlot | undefined;

	const fmt = $derived(format ?? ((v: number) => `${Math.round(v * 100) / 100}${unit}`));

	function build(width: number) {
		plot?.destroy();
		if (!host) return;
		const opts: uPlot.Options = {
			width,
			height,
			cursor: { drag: { x: false, y: false } },
			legend: { show: true },
			scales: {
				x: { time: true },
				y: {
					range: (_u, min, hi) => {
						const top = max !== undefined ? Math.max(max, hi) : hi;
						return [Math.min(0, min), top <= 0 ? 1 : top * 1.1];
					}
				}
			},
			axes: [
				{ stroke: '#6b7280', grid: { stroke: '#f3f4f6' }, ticks: { stroke: '#e5e7eb' } },
				{
					stroke: '#6b7280',
					grid: { stroke: '#f3f4f6' },
					ticks: { stroke: '#e5e7eb' },
					size: 56,
					values: (_u, ticks) => ticks.map((t) => fmt(t))
				}
			],
			series: [
				{ label: 'time' },
				...series.map((s) => ({
					label: s.label,
					stroke: s.color,
					width: 1.5,
					dash: s.dash,
					fill: s.fill ? s.color + '22' : undefined,
					value: (_u: uPlot, v: number | null) => (v === null ? '–' : fmt(v))
				}))
			]
		};
		// Initial data is read untracked; the setData effect below owns updates.
		const data = untrack(() => [times, ...values] as uPlot.AlignedData);
		plot = new uPlot(opts, data, host);
	}

	$effect(() => {
		if (!host) return;
		const el = host;
		// Depend on the series shape so a change of definitions rebuilds.
		void series.length;
		build(el.clientWidth || 600);
		const ro = new ResizeObserver(() => {
			plot?.setSize({ width: el.clientWidth || 600, height });
		});
		ro.observe(el);
		return () => {
			ro.disconnect();
			plot?.destroy();
			plot = undefined;
		};
	});

	$effect(() => {
		const data = [times, ...values] as uPlot.AlignedData;
		plot?.setData(data);
	});
</script>

<div bind:this={host} class="w-full text-xs"></div>
