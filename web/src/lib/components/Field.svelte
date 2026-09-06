<!-- Label, control and hint. Labels are sentence case, never shouting. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		hint,
		for: htmlFor,
		class: klass = '',
		children
	}: { label: string; hint?: string; for?: string; class?: string; children: Snippet } = $props();
</script>

<div class="flex flex-col gap-2 {klass}">
	<label for={htmlFor} class="text-sm font-medium text-ink">{label}</label>
	{@render children()}
	{#if hint}
		<p class="text-xs text-ink-3">{hint}</p>
	{/if}
</div>
