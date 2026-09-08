<!-- Action menu: a trigger and a floating list of actions. The default
     trigger is the 44px "more" button; pass `children` to draw your own
     trigger content (the account button in the topbar). `header` renders a
     non-interactive block above the items. The panel floats in viewport
     coordinates so tables and the content pane never clip it; arrow keys
     move between items; it closes on outside click, escape, scroll, or
     after an item runs. -->
<script lang="ts">
	import { tick, type Snippet } from 'svelte';
	import { placePanel, type Placement } from '$lib/popover';
	import Icon, { type IconName } from './Icon.svelte';

	export type MenuItem =
		| { divider: true }
		| {
				divider?: false;
				label: string;
				icon?: IconName;
				href?: string;
				external?: boolean;
				onclick?: () => void;
				danger?: boolean;
				disabled?: boolean;
		  };

	let {
		items,
		label = 'More actions',
		icon = 'more',
		align = 'right',
		size = 'md',
		triggerClass = '',
		header,
		children
	}: {
		items: MenuItem[];
		label?: string;
		icon?: IconName;
		align?: 'left' | 'right';
		size?: 'sm' | 'md';
		/** Classes for a custom trigger (with `children`); replaces the icon button look. */
		triggerClass?: string;
		header?: Snippet;
		children?: Snippet<[{ open: boolean }]>;
	} = $props();

	let open = $state(false);
	let trigger: HTMLButtonElement | undefined = $state();
	let panel: HTMLDivElement | undefined = $state();
	let pos = $state<Placement | null>(null);

	function focusables(): HTMLElement[] {
		return Array.from(
			panel?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? []
		);
	}

	async function show(focusFirst = false) {
		open = true;
		await tick();
		if (trigger && panel) pos = placePanel(trigger, panel, align);
		if (focusFirst) focusables()[0]?.focus();
	}
	function hide(refocus = false) {
		if (!open) return;
		open = false;
		pos = null;
		if (refocus) trigger?.focus();
	}

	function onTriggerKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			show(true);
		}
	}
	function onPanelKey(e: KeyboardEvent) {
		const list = focusables();
		const i = list.indexOf(document.activeElement as HTMLElement);
		switch (e.key) {
			case 'ArrowDown':
				e.preventDefault();
				list[(i + 1) % list.length]?.focus();
				break;
			case 'ArrowUp':
				e.preventDefault();
				list[(i - 1 + list.length) % list.length]?.focus();
				break;
			case 'Home':
				e.preventDefault();
				list[0]?.focus();
				break;
			case 'End':
				e.preventDefault();
				list[list.length - 1]?.focus();
				break;
			case 'Escape':
				e.preventDefault();
				hide(true);
				break;
			case 'Tab':
				hide();
				break;
		}
	}

	function onWindowPointer(e: PointerEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (trigger?.contains(t) || panel?.contains(t)) return;
		hide();
	}
	function onScroll(e: Event) {
		if (open && !(panel && e.target instanceof Node && panel.contains(e.target))) hide();
	}

	const defaultTrigger = $derived(
		`inline-flex items-center justify-center text-ink-2 transition-colors hover:bg-sunken hover:text-ink ${
			size === 'sm' ? 'size-9 rounded-control-sm' : 'size-11 rounded-control'
		} ${open ? 'bg-sunken text-ink' : ''}`
	);
</script>

<svelte:window onpointerdown={onWindowPointer} onresize={() => hide()} onscrollcapture={onScroll} />

<button
	bind:this={trigger}
	type="button"
	class={children ? triggerClass : defaultTrigger}
	aria-label={children ? undefined : label}
	aria-haspopup="menu"
	aria-expanded={open}
	onkeydown={onTriggerKey}
	onclick={(e) => {
		e.stopPropagation();
		if (open) hide();
		else show();
	}}
>
	{#if children}
		{@render children({ open })}
	{:else}
		<Icon name={icon} size={18} />
	{/if}
</button>

{#if open}
	<div
		bind:this={panel}
		role="menu"
		tabindex="-1"
		aria-label={label}
		onkeydown={onPanelKey}
		class="fixed z-50 min-w-56 rounded-plate bg-pop p-2 shadow-pop outline-none {pos
			? 'opacity-100'
			: 'opacity-0'}"
		style="top:{pos?.top ?? 0}px; left:{pos?.left ?? 0}px"
	>
		{#if header}
			<div class="px-3 pt-2 pb-2.5">{@render header()}</div>
			<div class="mx-1 mb-1.5 h-px bg-rule"></div>
		{/if}
		{#each items as item, i (i)}
			{#if item.divider}
				<div class="mx-1 my-1.5 h-px bg-rule"></div>
			{:else if item.href}
				<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- callers pass resolved paths -->
				<a
					href={item.href}
					target={item.external ? '_blank' : undefined}
					rel={item.external ? 'noreferrer noopener' : undefined}
					role="menuitem"
					tabindex="-1"
					onclick={() => hide()}
					class="flex items-center gap-3 rounded-control-sm px-3 py-2.5 text-sm font-medium text-ink outline-none hover:bg-sunken focus-visible:bg-sunken"
				>
					{#if item.icon}<Icon name={item.icon} size={18} class="text-ink-3" />{/if}
					<span class="flex-1">{item.label}</span>
					{#if item.external}<Icon name="external" size={14} class="text-ink-3" />{/if}
				</a>
			{:else}
				<button
					type="button"
					role="menuitem"
					tabindex="-1"
					disabled={item.disabled}
					onclick={() => {
						hide();
						item.onclick?.();
					}}
					class="flex w-full items-center gap-3 rounded-control-sm px-3 py-2.5 text-left text-sm font-medium outline-none disabled:cursor-not-allowed disabled:opacity-50 {item.danger
						? 'text-danger-ink hover:bg-danger-soft focus-visible:bg-danger-soft'
						: 'text-ink hover:bg-sunken focus-visible:bg-sunken'}"
				>
					{#if item.icon}
						<Icon name={item.icon} size={18} class={item.danger ? '' : 'text-ink-3'} />
					{/if}
					{item.label}
				</button>
			{/if}
		{/each}
	</div>
{/if}
