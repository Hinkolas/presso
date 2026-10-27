<!-- Boolean switch. -->
<script lang="ts">
	let {
		checked = $bindable(false),
		disabled = false,
		label,
		hint,
		id,
		onchange
	}: {
		checked?: boolean;
		disabled?: boolean;
		label?: string;
		hint?: string;
		id?: string;
		/** Called with the new state after the user flips the switch. */
		onchange?: (checked: boolean) => void;
	} = $props();
</script>

<label
	class="group inline-flex items-start gap-3 select-none {disabled
		? 'cursor-not-allowed opacity-50'
		: 'cursor-pointer'}"
>
	<input
		type="checkbox"
		role="switch"
		{id}
		bind:checked
		{disabled}
		onchange={() => onchange?.(checked)}
		class="peer sr-only"
	/>
	<span
		class="relative mt-0.5 inline-block h-6 w-11 shrink-0 rounded-full transition-[background-color,box-shadow] duration-150 peer-focus-visible:ring-3 peer-focus-visible:ring-brand/20 after:absolute after:top-0.5 after:left-0.5 after:size-5 after:rounded-full after:bg-surface after:shadow-[0_1px_2px_rgb(22_24_29/0.25)] after:transition-transform after:duration-150 after:ease-out {checked
			? 'bg-brand after:translate-x-5'
			: 'bg-rule-strong group-hover:bg-ink-3/60'}"
		aria-hidden="true"
	></span>
	{#if label}
		<span class="flex flex-col">
			<span class="text-sm font-medium text-ink">{label}</span>
			{#if hint}<span class="text-xs text-ink-3">{hint}</span>{/if}
		</span>
	{/if}
</label>
