<!-- Centered dialog on a backdrop; closes on Escape or backdrop click. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		children,
		footer
	}: { open?: boolean; title: string; children: Snippet; footer?: Snippet } = $props();

	let dialog: HTMLDialogElement | undefined = $state();

	$effect(() => {
		if (!dialog) return;
		if (open && !dialog.open) dialog.showModal();
		else if (!open && dialog.open) dialog.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === dialog) open = false;
	}
</script>

<dialog
	bind:this={dialog}
	onclose={() => (open = false)}
	onclick={onBackdrop}
	class="m-auto w-full max-w-md rounded-lg border border-gray-200 bg-white p-0 shadow-xl backdrop:bg-gray-900/40"
>
	{#if open}
		<div class="p-5">
			<h2 class="text-base font-semibold text-gray-900">{title}</h2>
			<div class="mt-3 text-sm text-gray-700">
				{@render children()}
			</div>
			{#if footer}
				<div class="mt-5 flex justify-end gap-2">
					{@render footer()}
				</div>
			{/if}
		</div>
	{/if}
</dialog>
