<!-- A thin track with a filled part. `value` is 0..1; leave it null for an
     indeterminate sweep while nothing is known yet. Brass while in motion,
     green when complete, red when it stopped short. -->
<script lang="ts">
	let {
		value = null,
		tone = 'busy',
		size = 'md',
		label,
		class: klass = ''
	}: {
		value?: number | null;
		tone?: 'busy' | 'ok' | 'danger';
		size?: 'sm' | 'md';
		/** Accessible name; without it the bar is decorative. */
		label?: string;
		class?: string;
	} = $props();

	const fill = { busy: 'bg-busy', ok: 'bg-ok', danger: 'bg-danger' };
	const pct = $derived(value === null ? 0 : Math.round(Math.min(1, Math.max(0, value)) * 100));
</script>

<div
	class="overflow-hidden rounded-full bg-rule {size === 'sm' ? 'h-1' : 'h-1.5'} {klass}"
	role={label ? 'progressbar' : undefined}
	aria-label={label}
	aria-valuemin={label ? 0 : undefined}
	aria-valuemax={label ? 100 : undefined}
	aria-valuenow={label && value !== null ? pct : undefined}
>
	{#if value === null}
		<div class="sweep h-full w-1/3 rounded-full {fill[tone]}"></div>
	{:else}
		<div
			class="h-full rounded-full transition-[width] duration-500 ease-out {fill[tone]}"
			style="width:{pct}%"
		></div>
	{/if}
</div>

<style>
	@keyframes sweep {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(300%);
		}
	}
	.sweep {
		animation: sweep 1.8s ease-in-out infinite;
	}
</style>
