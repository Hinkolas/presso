<!-- Hairline trend, no axes. Fills under the line so a flat zero still reads
     as "measured, nothing happened" rather than "missing". -->
<script lang="ts">
	let {
		values,
		width = 88,
		height = 26,
		tone = 'brand',
		class: klass = ''
	}: {
		values: number[];
		width?: number;
		height?: number;
		tone?: 'brand' | 'ok' | 'busy' | 'danger' | 'ink-3';
		class?: string;
	} = $props();

	const geometry = $derived.by(() => {
		const n = values.length;
		if (n === 0) return { line: '', area: '' };
		const max = Math.max(1, ...values);
		const pad = 1.5;
		const w = width - pad * 2;
		const h = height - pad * 2;
		const pts = values.map((v, i) => {
			const x = pad + (n === 1 ? w / 2 : (i / (n - 1)) * w);
			const y = pad + h - (v / max) * h;
			return [x, y] as const;
		});
		const line = pts
			.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`)
			.join(' ');
		const base = (height - pad).toFixed(1);
		const area = `${line} L${pts[pts.length - 1][0].toFixed(1)} ${base} L${pts[0][0].toFixed(1)} ${base} Z`;
		return { line, area };
	});
	const color = $derived(`var(--color-${tone})`);
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" class="block {klass}" aria-hidden="true">
	{#if geometry.line}
		<path d={geometry.area} fill={color} opacity="0.12" />
		<path
			d={geometry.line}
			fill="none"
			stroke={color}
			stroke-width="1.25"
			stroke-linejoin="round"
			stroke-linecap="round"
		/>
	{:else}
		<line
			x1="2"
			y1={height / 2}
			x2={width - 2}
			y2={height / 2}
			stroke="var(--color-rule-strong)"
			stroke-width="1"
			stroke-dasharray="2 3"
		/>
	{/if}
</svg>
