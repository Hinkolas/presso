<!-- Tiny inline SVG line for a series of numbers. -->
<script lang="ts">
	let {
		values,
		width = 80,
		height = 20,
		stroke = '#4b5563'
	}: { values: number[]; width?: number; height?: number; stroke?: string } = $props();

	const points = $derived.by(() => {
		if (values.length === 0) return '';
		const max = Math.max(...values, 1);
		const step = values.length > 1 ? width / (values.length - 1) : 0;
		return values
			.map(
				(v, i) => `${(i * step).toFixed(1)},${(height - (v / max) * (height - 2) - 1).toFixed(1)}`
			)
			.join(' ');
	});
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" class="block" aria-hidden="true">
	{#if points}
		<polyline fill="none" {stroke} stroke-width="1.5" stroke-linejoin="round" {points} />
	{/if}
</svg>
