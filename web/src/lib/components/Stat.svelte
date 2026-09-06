<!-- A number with its meaning. Big where it matters, quiet otherwise. -->
<script lang="ts">
	let {
		label,
		value,
		sub,
		size = 'md',
		tone = 'ink',
		class: klass = ''
	}: {
		label: string;
		value: string;
		sub?: string;
		size?: 'md' | 'lg';
		tone?: 'ink' | 'ok' | 'busy' | 'danger' | 'ink-2';
		class?: string;
	} = $props();
	const tones = {
		ink: 'text-ink',
		ok: 'text-ok-ink',
		busy: 'text-busy-ink',
		danger: 'text-danger-ink',
		'ink-2': 'text-ink-2'
	};
</script>

<div class="flex min-w-0 flex-col {klass}">
	<span class="text-xs text-ink-2">{label}</span>
	<span
		class="truncate font-semibold tracking-[-0.01em] num {tones[tone]} {size === 'lg'
			? 'mt-1 text-3xl'
			: 'mt-0.5 text-xl'}"
	>
		{value}
	</span>
	{#if sub}
		<span class="mt-0.5 truncate text-xs text-ink-3">{sub}</span>
	{/if}
</div>
