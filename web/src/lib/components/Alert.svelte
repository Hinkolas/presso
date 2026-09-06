<!-- Inline notice for errors, warnings and confirmations. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		tone = 'error',
		children
	}: { tone?: 'error' | 'warn' | 'info' | 'success'; children: Snippet } = $props();

	const tones = {
		error: 'border-red-200 bg-red-50 text-red-800',
		warn: 'border-amber-200 bg-amber-50 text-amber-800',
		info: 'border-blue-200 bg-blue-50 text-blue-800',
		success: 'border-green-200 bg-green-50 text-green-800'
	};
</script>

<div role="alert" class="rounded-md border px-3 py-2 text-sm {tones[tone]}">
	{@render children()}
</div>
