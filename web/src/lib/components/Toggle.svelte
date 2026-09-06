<!-- Boolean switch bound to `checked`. -->
<script lang="ts">
	let {
		checked = $bindable(false),
		disabled = false,
		label,
		id
	}: { checked?: boolean; disabled?: boolean; label?: string; id?: string } = $props();
</script>

<label
	class="inline-flex cursor-pointer items-center gap-2 select-none {disabled ? 'opacity-50' : ''}"
>
	<input type="checkbox" {id} bind:checked {disabled} class="peer sr-only" />
	<span
		class="relative inline-block h-5 w-9 rounded-full bg-gray-300 transition-colors peer-checked:bg-gray-900 peer-focus-visible:ring-2 peer-focus-visible:ring-gray-400 after:absolute after:top-0.5 after:left-0.5 after:size-4 after:rounded-full after:bg-white after:transition-transform peer-checked:after:translate-x-4"
	></span>
	{#if label}
		<span class="text-sm text-gray-800">{label}</span>
	{/if}
</label>
