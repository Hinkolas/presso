<!-- Label, control and hint. Labels are sentence case, never shouting. An
     `error` replaces the hint; the control should point at it with
     aria-describedby="{for}-error" and set aria-invalid. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		label,
		hint,
		error,
		for: htmlFor,
		class: klass = '',
		children
	}: {
		label: string;
		hint?: string;
		error?: string;
		for?: string;
		class?: string;
		children: Snippet;
	} = $props();
</script>

<div class="flex flex-col gap-2 {klass}">
	<label for={htmlFor} class="text-sm font-medium text-ink">{label}</label>
	{@render children()}
	{#if error}
		<p id={htmlFor ? `${htmlFor}-error` : undefined} class="text-xs text-danger-ink">{error}</p>
	{:else if hint}
		<p id={htmlFor ? `${htmlFor}-hint` : undefined} class="text-xs text-ink-3">{hint}</p>
	{/if}
</div>
