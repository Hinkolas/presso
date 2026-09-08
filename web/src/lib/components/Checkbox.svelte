<!-- Checkbox with its label and hint. The native input stays in the tree
     for the keyboard and screen readers; the box you see is drawn here. -->
<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		checked = $bindable(false),
		disabled = false,
		label,
		hint,
		id
	}: {
		checked?: boolean;
		disabled?: boolean;
		label?: string;
		hint?: string;
		id?: string;
	} = $props();
</script>

<label
	class="group inline-flex items-start gap-3 select-none {disabled
		? 'cursor-not-allowed opacity-50'
		: 'cursor-pointer'}"
>
	<input type="checkbox" {id} bind:checked {disabled} class="peer sr-only" />
	<span
		class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-key border transition-[background-color,border-color,box-shadow] duration-150 peer-focus-visible:ring-3 peer-focus-visible:ring-brand/20 {checked
			? 'border-brand bg-brand text-on-brand'
			: 'border-rule-strong bg-surface group-hover:border-ink-3'}"
		aria-hidden="true"
	>
		{#if checked}
			<Icon name="check" size={13} class="[stroke-width:2.5]" />
		{/if}
	</span>
	{#if label}
		<span class="flex flex-col">
			<span class="text-sm font-medium text-ink">{label}</span>
			{#if hint}<span class="text-xs text-ink-3">{hint}</span>{/if}
		</span>
	{/if}
</label>
