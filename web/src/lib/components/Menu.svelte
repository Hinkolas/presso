<!-- Overflow menu: a trigger and a list of actions. The panel floats in
     viewport coordinates so tables and the content pane never clip it;
     closes on outside click, escape, scroll, or after an item runs. -->
<script lang="ts">
	import { tick } from 'svelte';
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
		size = 'md'
	}: {
		items: MenuItem[];
		label?: string;
		icon?: IconName;
		align?: 'left' | 'right';
		size?: 'sm' | 'md';
	} = $props();

	let open = $state(false);
	let trigger: HTMLButtonElement | undefined = $state();
	let panel: HTMLDivElement | undefined = $state();
	let pos = $state<Placement | null>(null);

	async function show() {
		open = true;
		await tick();
		if (trigger && panel) pos = placePanel(trigger, panel, align);
	}
	function hide() {
		open = false;
		pos = null;
	}

	function onWindowPointer(e: PointerEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (trigger?.contains(t) || panel?.contains(t)) return;
		hide();
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') hide();
	}
	function onScroll(e: Event) {
		if (open && !(panel && e.target instanceof Node && panel.contains(e.target))) hide();
	}
</script>

<svelte:window
	onpointerdown={onWindowPointer}
	onkeydown={onKey}
	onresize={hide}
	onscrollcapture={onScroll}
/>

<button
	bind:this={trigger}
	type="button"
	class="inline-flex items-center justify-center text-ink-2 transition-colors hover:bg-sunken hover:text-ink {size ===
	'sm'
		? 'size-9 rounded-control-sm'
		: 'size-11 rounded-control'} {open ? 'bg-sunken text-ink' : ''}"
	aria-label={label}
	aria-haspopup="menu"
	aria-expanded={open}
	onclick={(e) => {
		e.stopPropagation();
		if (open) hide();
		else show();
	}}
>
	<Icon name={icon} size={18} />
</button>

{#if open}
	<div
		bind:this={panel}
		class="fixed z-50 min-w-56 rounded-plate bg-surface p-2 shadow-pop {pos
			? 'opacity-100'
			: 'opacity-0'}"
		style="top:{pos?.top ?? 0}px; left:{pos?.left ?? 0}px"
		role="menu"
	>
		{#each items as item, i (i)}
			{#if item.divider}
				<div class="my-1.5 h-px bg-rule"></div>
			{:else if item.href}
				<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- callers pass resolved paths -->
				<a
					href={item.href}
					target={item.external ? '_blank' : undefined}
					rel={item.external ? 'noreferrer noopener' : undefined}
					role="menuitem"
					onclick={hide}
					class="flex items-center gap-3 rounded-control-sm px-3 py-2.5 text-sm font-medium text-ink hover:bg-sunken"
				>
					{#if item.icon}<Icon name={item.icon} size={18} class="text-ink-3" />{/if}
					<span class="flex-1">{item.label}</span>
					{#if item.external}<Icon name="external" size={14} class="text-ink-3" />{/if}
				</a>
			{:else}
				<button
					type="button"
					role="menuitem"
					disabled={item.disabled}
					onclick={() => {
						hide();
						item.onclick?.();
					}}
					class="flex w-full items-center gap-3 rounded-control-sm px-3 py-2.5 text-left text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50 {item.danger
						? 'text-danger-ink hover:bg-danger-soft'
						: 'text-ink hover:bg-sunken'}"
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
