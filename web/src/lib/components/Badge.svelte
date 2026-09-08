<!-- Small tinted label for a fact about a thing: "Latest", "Legacy", "This
     device". Not a status: those are StatusPill. `sm` is the inline chip,
     `md` matches the 30px soft StatusPill so the two can sit side by side. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		tone = 'neutral',
		size = 'sm',
		title,
		class: klass = '',
		children
	}: {
		tone?: 'brand' | 'info' | 'neutral' | 'busy' | 'danger';
		size?: 'sm' | 'md';
		title?: string;
		class?: string;
		children: Snippet;
	} = $props();

	const tones = {
		brand: 'bg-brand-soft text-brand',
		info: 'bg-info-soft text-info-ink',
		neutral: 'bg-sunken text-ink-2',
		busy: 'bg-busy-soft text-busy-ink',
		danger: 'bg-danger-soft text-danger-ink'
	};
	const sizes = {
		sm: 'px-2.5 py-0.5 text-2xs',
		md: 'h-[30px] px-3 text-xs'
	};
</script>

<span
	class="inline-flex shrink-0 items-center rounded-full font-semibold whitespace-nowrap {tones[
		tone
	]} {sizes[size]} {klass}"
	{title}
>
	{@render children()}
</span>
