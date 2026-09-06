<!-- Dialog on the native <dialog>: escape and backdrop click close it. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		open = $bindable(false),
		title,
		children,
		footer
	}: { open?: boolean; title: string; children: Snippet; footer?: Snippet } = $props();

	let el: HTMLDialogElement | undefined = $state();

	$effect(() => {
		if (!el) return;
		if (open && !el.open) el.showModal();
		else if (!open && el.open) el.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === el) open = false;
	}
</script>

<dialog
	bind:this={el}
	onclose={() => (open = false)}
	onclick={onBackdrop}
	class="m-auto w-[min(34rem,calc(100vw-2rem))] rounded-dialog bg-surface p-0 text-ink shadow-dialog backdrop:bg-scrim open:animate-[dialog-in_180ms_ease-out]"
>
	{#if open}
		<div class="flex items-start justify-between gap-4 px-7 pt-6">
			<h2 class="display text-2xl">{title}</h2>
			<button
				type="button"
				class="-mt-1 -mr-2 rounded-control-sm p-2 text-ink-3 hover:bg-sunken hover:text-ink"
				onclick={() => (open = false)}
				aria-label="Close"
			>
				<Icon name="close" size={18} />
			</button>
		</div>
		<div class="px-7 py-5 text-sm leading-6 text-ink-2">
			{@render children()}
		</div>
		{#if footer}
			<div class="flex justify-end gap-3 border-t border-rule px-7 py-5">
				{@render footer()}
			</div>
		{/if}
	{/if}
</dialog>

<style>
	@keyframes dialog-in {
		from {
			opacity: 0;
			transform: translateY(6px) scale(0.985);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}
</style>
