<!-- The mark: a small press block and the name in the serif. -->
<script lang="ts">
	let { size = 'md' }: { size?: 'md' | 'lg' } = $props();
</script>

<span class="inline-flex items-center gap-3 text-ink">
	<span
		class="grid shrink-0 place-items-center bg-brand {size === 'lg'
			? 'size-9 rounded-[9px]'
			: 'size-7 rounded-[7px]'}"
		aria-hidden="true"
	>
		<span class="block rounded-[2px] bg-on-brand {size === 'lg' ? 'h-2.5 w-4' : 'h-2 w-3'}"></span>
	</span>
	<span class="display {size === 'lg' ? 'text-3xl' : 'text-[26px] leading-none'}">presso</span>
</span>
