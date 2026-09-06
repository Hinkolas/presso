<!-- Boolean switch. -->
<script lang="ts">
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
	class="inline-flex cursor-pointer items-start gap-3 select-none {disabled
		? 'cursor-not-allowed opacity-50'
		: ''}"
>
	<input type="checkbox" {id} bind:checked {disabled} class="peer sr-only" />
	<span
		class="relative mt-0.5 inline-block h-6 w-11 shrink-0 rounded-full bg-rule-strong transition-colors peer-checked:bg-brand peer-focus-visible:ring-2 peer-focus-visible:ring-brand/40 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-surface after:absolute after:top-0.5 after:left-0.5 after:size-5 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:after:translate-x-5"
	></span>
	{#if label}
		<span class="flex flex-col">
			<span class="text-sm font-medium text-ink">{label}</span>
			{#if hint}<span class="text-xs text-ink-3">{hint}</span>{/if}
		</span>
	{/if}
</label>
