<!-- Label + control + hint wrapper for forms. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		hint,
		for: htmlFor,
		children
	}: { label: string; hint?: string; for?: string; children: Snippet } = $props();
</script>

<div class="flex flex-col gap-1">
	<label for={htmlFor} class="text-xs font-medium text-gray-700">{label}</label>
	{@render children()}
	{#if hint}
		<p class="text-xs text-gray-500">{hint}</p>
	{/if}
</div>
