<!-- uPlot wrapper: time on x, numeric series on y, colors from the theme
     tokens so charts follow light and dark mode. Rebuilds when the series
     definitions or the color scheme change; data updates in place. -->
<script lang="ts">
	import { untrack } from 'svelte';
	import uPlot from 'uplot';
	import 'uplot/dist/uPlot.min.css';

	export type SeriesTone = 'brand' | 'ok' | 'busy' | 'danger' | 'info' | 'ink-2' | 'ink-3';
	export type Series = { label: string; tone: SeriesTone; dash?: number[]; fill?: boolean };

	let {
		times,
		series,
		values,
		height = 200,
		format,
		max,
		xRange
	}: {
		/** Unix seconds, ascending. */
		times: number[];
		series: Series[];
		/** One array per series, aligned with `times`; null for gaps. */
		values: (number | null)[][];
		height?: number;
		format?: (v: number) => string;
		max?: number;
		/** Unix seconds; pins the time axis to a window even with sparse data. */
		xRange?: [number, number];
	} = $props();

	let host: HTMLDivElement | undefined = $state();
	let plot: uPlot | undefined;
	let scheme = $state(0);

	const fmt = $derived(format ?? ((v: number) => `${Math.round(v * 100) / 100}`));

	function token(name: string): string {
		return getComputedStyle(document.documentElement).getPropertyValue(`--color-${name}`).trim();
	}

	function withAlpha(hex: string, alpha: number): string {
		if (!hex.startsWith('#') || hex.length !== 7) return hex;
		const n = parseInt(hex.slice(1), 16);
		return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
	}

	function build(width: number) {
		plot?.destroy();
		if (!host) return;
		const axis = token('ink-3');
		const grid = token('rule');
		const opts: uPlot.Options = {
			width,
			height,
			cursor: { drag: { x: false, y: false }, points: { size: 6 } },
			legend: { show: true, live: true },
			scales: {
				x: { time: true, ...(xRange ? { range: () => xRange } : {}) },
				y: {
					range: (_u, min, hi) => {
						const top = max !== undefined ? Math.max(max, hi) : hi;
						return [Math.min(0, min), top <= 0 ? 1 : top * 1.12];
					}
				}
			},
			axes: [
				{
					stroke: axis,
					font: '11px "Instrument Sans Variable", sans-serif',
					grid: { show: false },
					ticks: { stroke: grid, size: 4 }
				},
				{
					stroke: axis,
					font: '11px "Instrument Sans Variable", sans-serif',
					grid: { stroke: grid, width: 1 },
					ticks: { show: false },
					size: 52,
					values: (_u, ticks) => ticks.map((t) => fmt(t))
				}
			],
			series: [
				{ label: 'time' },
				...series.map((s) => {
					const c = token(s.tone);
					return {
						label: s.label,
						stroke: c,
						width: 1.5,
						dash: s.dash,
						fill: s.fill ? withAlpha(c, 0.1) : undefined,
						value: (_u: uPlot, v: number | null) => (v === null ? '–' : fmt(v))
					};
				})
			]
		};
		const data = untrack(() => [times, ...values] as uPlot.AlignedData);
		plot = new uPlot(opts, data, host);
	}

	$effect(() => {
		const mq = window.matchMedia('(prefers-color-scheme: dark)');
		const onChange = () => scheme++;
		mq.addEventListener('change', onChange);
		return () => mq.removeEventListener('change', onChange);
	});

	$effect(() => {
		if (!host) return;
		const el = host;
		void series.length;
		void scheme;
		void xRange;
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

<div bind:this={host} class="chart w-full"></div>

<style>
	.chart :global(.u-legend) {
		font:
			12px 'Instrument Sans Variable',
			sans-serif;
		color: var(--color-ink-2);
		text-align: left;
		padding-top: 6px;
	}
	.chart :global(.u-legend .u-series) {
		padding: 0 12px 0 0;
	}
	.chart :global(.u-legend .u-series:first-child) {
		display: none;
	}
	.chart :global(.u-legend .u-marker) {
		border-radius: 2px;
		width: 10px;
		height: 10px;
	}
	.chart :global(.u-legend .u-value) {
		font-variant-numeric: tabular-nums;
		color: var(--color-ink);
	}
	.chart :global(.u-select) {
		background: color-mix(in srgb, var(--color-brand) 10%, transparent);
	}
</style>
