<!-- A plate with an optional title row. Leave `title` off for a bare plate. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		subtitle,
		actions,
		padded = true,
		id,
		class: klass = '',
		children
	}: {
		title?: string;
		subtitle?: string;
		actions?: Snippet;
		padded?: boolean;
		id?: string;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<section {id} class="flex flex-col frame {klass}">
	{#if title || actions}
		<header class="flex items-center justify-between gap-4 border-b border-rule px-6 py-4">
			<div class="min-w-0">
				{#if title}
					<h2 class="display text-lg text-ink">{title}</h2>
				{/if}
				{#if subtitle}
					<p class="mt-0.5 text-xs text-ink-2">{subtitle}</p>
				{/if}
			</div>
			{#if actions}
				<div class="flex shrink-0 items-center gap-2">{@render actions()}</div>
			{/if}
		</header>
	{/if}
	<div class="min-h-0 flex-1 {padded ? 'p-6' : ''}">
		{@render children()}
	</div>
</section>
