<!-- Bordered content panel with an optional title row. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		actions,
		padded = true,
		class: klass = '',
		children
	}: {
		title?: string;
		actions?: Snippet;
		padded?: boolean;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<section class="rounded-lg border border-gray-200 bg-white {klass}">
	{#if title || actions}
		<header class="flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-2.5">
			{#if title}
				<h2 class="text-sm font-semibold text-gray-900">{title}</h2>
			{/if}
			{#if actions}
				<div class="flex items-center gap-2">{@render actions()}</div>
			{/if}
		</header>
	{/if}
	<div class={padded ? 'p-4' : ''}>
		{@render children()}
	</div>
</section>
