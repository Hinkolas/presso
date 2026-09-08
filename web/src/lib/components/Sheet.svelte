<!-- Side panel on the native <dialog>: a plate that slides in at the right
     edge for detail that belongs beside the page rather than on top of it
     (a job's timeline). Escape, the close button and the backdrop dismiss
     it; the body scrolls, the footer stays. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from './Button.svelte';

	let {
		open = $bindable(false),
		title,
		subtitle,
		aside,
		children,
		footer
	}: {
		open?: boolean;
		title: string;
		subtitle?: string;
		/** Rendered next to the title: a status pill, a badge. */
		aside?: Snippet;
		children: Snippet;
		footer?: Snippet;
	} = $props();

	let el: HTMLDialogElement | undefined = $state();
	let body: HTMLDivElement | undefined = $state();

	$effect(() => {
		if (!el) return;
		if (open && !el.open) {
			el.showModal();
			// Start reading at the top rather than on the close button.
			body?.focus();
		} else if (!open && el.open) el.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === el) open = false;
	}
</script>

<dialog
	bind:this={el}
	onclose={() => (open = false)}
	onclick={onBackdrop}
	class="fixed inset-y-0 right-0 left-auto m-0 h-dvh max-h-none w-[min(30rem,100vw)] max-w-none flex-col bg-surface p-0 text-ink shadow-dialog backdrop:bg-scrim open:flex open:animate-[sheet-in_220ms_ease-out] sm:inset-y-3 sm:right-3 sm:h-auto sm:rounded-plate"
>
	{#if open}
		<header class="flex items-start justify-between gap-4 border-b border-rule px-6 pt-5 pb-4">
			<div class="min-w-0">
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
					<h2 class="display text-2xl leading-tight text-ink">{title}</h2>
					{#if aside}{@render aside()}{/if}
				</div>
				{#if subtitle}
					<p class="mt-1 text-xs text-ink-2">{subtitle}</p>
				{/if}
			</div>
			<Button
				variant="ghost"
				size="sm"
				icon="close"
				class="-mt-1 -mr-2.5 shrink-0"
				onclick={() => (open = false)}
				aria-label="Close"
			/>
		</header>
		<div
			bind:this={body}
			tabindex="-1"
			class="min-h-0 flex-1 overflow-y-auto px-6 py-5 outline-none"
		>
			{@render children()}
		</div>
		{#if footer}
			<footer class="flex flex-wrap items-center justify-end gap-3 border-t border-rule px-6 py-4">
				{@render footer()}
			</footer>
		{/if}
	{/if}
</dialog>

<style>
	@keyframes sheet-in {
		from {
			opacity: 0;
			transform: translateX(16px);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}
</style>
