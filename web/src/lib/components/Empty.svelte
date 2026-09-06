<!-- Empty state: an invitation to act, not a shrug. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		text,
		class: klass = '',
		children
	}: { title: string; text?: string; class?: string; children?: Snippet } = $props();
</script>

<div
	class="flex flex-col items-center justify-center gap-2 rounded-plate border border-dashed border-rule-strong px-6 py-20 text-center {klass}"
>
	<p class="display text-2xl text-ink">{title}</p>
	{#if text}
		<p class="max-w-md text-sm text-ink-2">{text}</p>
	{/if}
	{#if children}
		<div class="mt-4">{@render children()}</div>
	{/if}
</div>
