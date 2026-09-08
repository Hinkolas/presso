<!-- Dialog on the native <dialog>: escape and backdrop click close it unless
     `dismissable` is off (a submit in flight). `header` renders under the
     title, for a stepper or a subtitle; the body scrolls when the viewport
     is short. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		open = $bindable(false),
		title,
		size = 'md',
		dismissable = true,
		header,
		children,
		footer
	}: {
		open?: boolean;
		title: string;
		/** `md` is 34rem for confirmations; `lg` is 46rem for forms with steps. */
		size?: 'md' | 'lg';
		/** When false, escape, the close button and the backdrop do nothing. */
		dismissable?: boolean;
		header?: Snippet;
		children: Snippet;
		footer?: Snippet;
	} = $props();

	let el: HTMLDialogElement | undefined = $state();

	$effect(() => {
		if (!el) return;
		if (open && !el.open) el.showModal();
		else if (!open && el.open) el.close();
	});

	function dismiss() {
		if (dismissable) open = false;
	}
	function onBackdrop(e: MouseEvent) {
		if (e.target === el) dismiss();
	}
	function onCancel(e: Event) {
		if (!dismissable) e.preventDefault();
	}
	function onClose() {
		// The browser may close a dialog past a cancelled `cancel` (a second
		// escape without user activation); keep it up while not dismissable.
		if (!dismissable && open) queueMicrotask(() => el?.showModal());
		else open = false;
	}
</script>

<dialog
	bind:this={el}
	onclose={onClose}
	oncancel={onCancel}
	onclick={onBackdrop}
	class="m-auto rounded-dialog bg-surface p-0 text-ink shadow-dialog backdrop:bg-scrim open:animate-[dialog-in_180ms_ease-out] {size ===
	'lg'
		? 'w-[min(46rem,calc(100vw-2rem))]'
		: 'w-[min(34rem,calc(100vw-2rem))]'}"
>
	{#if open}
		<div class="flex items-start justify-between gap-4 px-7 pt-6">
			<h2 class="display text-2xl">{title}</h2>
			<button
				type="button"
				class="-mt-1 -mr-2 rounded-control-sm p-2 text-ink-3 hover:bg-sunken hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
				onclick={dismiss}
				disabled={!dismissable}
				aria-label="Close"
			>
				<Icon name="close" size={18} />
			</button>
		</div>
		{#if header}
			<div class="px-7 pt-4">
				{@render header()}
			</div>
		{/if}
		<div class="max-h-[calc(100dvh-14rem)] overflow-y-auto px-7 py-5 text-sm leading-6 text-ink-2">
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
