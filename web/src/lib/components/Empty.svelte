<!-- Empty-state placeholder with a title, line of copy and optional action. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let { title, text, children }: { title: string; text?: string; children?: Snippet } = $props();
</script>

<div
	class="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-gray-300 px-6 py-12 text-center"
>
	<p class="text-sm font-medium text-gray-900">{title}</p>
	{#if text}
		<p class="text-sm text-gray-500">{text}</p>
	{/if}
	{#if children}
		<div class="mt-2">{@render children()}</div>
	{/if}
</div>
