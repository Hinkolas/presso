<!-- Exclusive choice among a few short options (ranges, filters, views).
     Options may carry an icon; with `iconOnly` the label becomes a tooltip. -->
<script lang="ts" generics="T extends string">
	import Icon, { type IconName } from './Icon.svelte';

	let {
		options,
		value = $bindable(),
		size = 'md',
		iconOnly = false,
		label,
		onchange
	}: {
		options: { value: T; label: string; icon?: IconName }[];
		value: T;
		size?: 'sm' | 'md';
		iconOnly?: boolean;
		label?: string;
		onchange?: (v: T) => void;
	} = $props();
</script>

<div
	class="inline-flex items-center gap-0.5 border border-rule-strong bg-surface {size === 'sm'
		? 'h-9 rounded-control-sm p-[3px]'
		: 'h-11 rounded-control p-1'}"
	role="tablist"
	aria-label={label}
>
	{#each options as o (o.value)}
		<button
			type="button"
			role="tab"
			aria-selected={value === o.value}
			aria-label={iconOnly ? o.label : undefined}
			title={iconOnly ? o.label : undefined}
			onclick={() => {
				value = o.value;
				onchange?.(o.value);
			}}
			class="inline-flex h-full items-center justify-center gap-2 rounded-chip text-sm font-medium transition-colors duration-150 {iconOnly
				? 'w-10'
				: 'px-3.5'} {value === o.value
				? 'bg-ink text-surface'
				: 'text-ink-2 hover:bg-sunken hover:text-ink'}"
		>
			{#if o.icon}<Icon name={o.icon} size={size === 'sm' ? 16 : 18} />{/if}
			{#if !iconOnly}{o.label}{/if}
		</button>
	{/each}
</div>
