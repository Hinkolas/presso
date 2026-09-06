<!-- Inline notice. Errors say what happened and, where possible, what to do. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		tone = 'error',
		class: klass = '',
		children
	}: { tone?: 'error' | 'warn' | 'info' | 'success'; class?: string; children: Snippet } = $props();

	const tones = {
		error: 'border-danger/25 bg-danger-soft text-danger-ink',
		warn: 'border-busy/40 bg-busy-soft text-ink',
		info: 'border-info/25 bg-info-soft text-ink',
		success: 'border-ok/25 bg-ok-soft text-ink'
	};
</script>

<div role="alert" class="rounded-control border px-4 py-3 text-sm leading-6 {tones[tone]} {klass}">
	{@render children()}
</div>
