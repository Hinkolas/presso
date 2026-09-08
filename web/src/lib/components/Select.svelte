<!-- Custom select: a 44px trigger and a floating listbox in the plate style.
     Arrow keys move, Enter picks, Escape closes, typing jumps to a match.
     Disabled options stay visible (with their hint saying why) but are
     skipped by the keys and cannot be picked. -->
<script lang="ts" generics="T extends string">
	import { tick } from 'svelte';
	import { placePanel, type Placement } from '$lib/popover';
	import Icon from './Icon.svelte';

	let {
		options,
		value = $bindable(),
		id,
		label,
		disabled = false,
		block = false,
		size = 'md',
		class: klass = '',
		onchange
	}: {
		options: { value: T; label: string; hint?: string; disabled?: boolean }[];
		value: T;
		id?: string;
		/** Accessible name when there is no <label for>. */
		label?: string;
		disabled?: boolean;
		/** Fill the container width (forms) instead of hugging the label (toolbars). */
		block?: boolean;
		size?: 'sm' | 'md';
		class?: string;
		onchange?: (v: T) => void;
	} = $props();

	let open = $state(false);
	let cursor = $state(0);
	let trigger: HTMLButtonElement | undefined = $state();
	let panel: HTMLDivElement | undefined = $state();
	let pos = $state<Placement | null>(null);
	let typed = '';
	let typedAt = 0;

	const selected = $derived(options.find((o) => o.value === value));
	const listId = $derived(`${id ?? 'select'}-list`);

	/** Next enabled index from `from` (exclusive) in `dir`; `from` when there is none. */
	function step(from: number, dir: 1 | -1): number {
		for (let i = from + dir; i >= 0 && i < options.length; i += dir) {
			if (!options[i].disabled) return i;
		}
		return from;
	}
	function firstEnabled(): number {
		const i = options.findIndex((o) => !o.disabled);
		return i < 0 ? 0 : i;
	}
	function lastEnabled(): number {
		for (let i = options.length - 1; i >= 0; i--) if (!options[i].disabled) return i;
		return options.length - 1;
	}

	async function show() {
		if (disabled) return;
		cursor = Math.max(
			0,
			options.findIndex((o) => o.value === value)
		);
		open = true;
		await tick();
		if (trigger && panel) pos = placePanel(trigger, panel, 'left');
		panel?.focus();
	}
	function hide(refocus = true) {
		if (!open) return;
		open = false;
		pos = null;
		if (refocus) trigger?.focus();
	}
	function pick(i: number) {
		const o = options[i];
		if (!o || o.disabled) return;
		if (o.value !== value) {
			value = o.value;
			onchange?.(o.value);
		}
		hide();
	}

	function onTriggerKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === ' ' || e.key === 'Enter') {
			e.preventDefault();
			show();
		}
	}
	function onPanelKey(e: KeyboardEvent) {
		switch (e.key) {
			case 'ArrowDown':
				e.preventDefault();
				cursor = step(cursor, 1);
				break;
			case 'ArrowUp':
				e.preventDefault();
				cursor = step(cursor, -1);
				break;
			case 'Home':
				e.preventDefault();
				cursor = firstEnabled();
				break;
			case 'End':
				e.preventDefault();
				cursor = lastEnabled();
				break;
			case 'Enter':
			case ' ':
				e.preventDefault();
				pick(cursor);
				break;
			case 'Escape':
				e.preventDefault();
				hide();
				break;
			case 'Tab':
				hide(false);
				break;
			default:
				if (e.key.length === 1 && !e.metaKey && !e.ctrlKey) {
					const now = Date.now();
					typed = now - typedAt < 600 ? typed + e.key : e.key;
					typedAt = now;
					const i = options.findIndex(
						(o) => !o.disabled && o.label.toLowerCase().startsWith(typed.toLowerCase())
					);
					if (i >= 0) cursor = i;
				}
		}
	}

	function onWindowPointer(e: PointerEvent) {
		if (!open) return;
		const t = e.target as Node;
		if (trigger?.contains(t) || panel?.contains(t)) return;
		hide(false);
	}
	// Anything that moves the trigger (scrolling the pane, resizing) closes it.
	function onScroll(e: Event) {
		if (open && !(panel && e.target instanceof Node && panel.contains(e.target))) hide(false);
	}
</script>

<svelte:window
	onpointerdown={onWindowPointer}
	onresize={() => hide(false)}
	onscrollcapture={onScroll}
/>

<button
	bind:this={trigger}
	{id}
	type="button"
	role="combobox"
	aria-haspopup="listbox"
	aria-expanded={open}
	aria-controls={open ? listId : undefined}
	aria-label={label}
	{disabled}
	onclick={() => (open ? hide() : show())}
	onkeydown={onTriggerKey}
	class="inline-flex items-center justify-between gap-3 border field-focus border-rule-strong bg-surface text-left text-sm font-medium text-ink transition-[background-color,border-color,box-shadow] duration-150 hover:bg-sunken disabled:cursor-not-allowed disabled:bg-sunken disabled:text-ink-3 {size ===
	'sm'
		? 'h-9 rounded-control-sm pr-2.5 pl-3'
		: 'h-11 rounded-control pr-3 pl-3.5'} {block ? 'w-full' : ''} {open
		? 'border-brand ring-3 ring-brand/20'
		: ''} {klass}"
>
	<span class="truncate {selected ? '' : 'text-ink-3'}">{selected?.label ?? 'Choose'}</span>
	<Icon
		name="chevron-down"
		size={16}
		class="shrink-0 text-ink-3 transition-transform {open ? 'rotate-180' : ''}"
	/>
</button>

{#if open}
	<div
		bind:this={panel}
		id={listId}
		role="listbox"
		tabindex="-1"
		aria-activedescendant="{listId}-{cursor}"
		onkeydown={onPanelKey}
		class="fixed z-50 max-h-80 overflow-y-auto rounded-plate bg-surface p-2 shadow-pop outline-none {pos
			? 'opacity-100'
			: 'opacity-0'}"
		style="top:{pos?.top ?? 0}px; left:{pos?.left ?? 0}px; min-width:{Math.max(
			200,
			pos?.minWidth ?? 0
		)}px"
	>
		{#each options as o, i (o.value)}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<div
				id="{listId}-{i}"
				role="option"
				tabindex="-1"
				aria-selected={o.value === value}
				aria-disabled={o.disabled || undefined}
				onpointerenter={() => {
					if (!o.disabled) cursor = i;
				}}
				onclick={() => pick(i)}
				class="flex items-center gap-3 rounded-control-sm px-3 py-2.5 text-sm {o.disabled
					? 'cursor-not-allowed text-ink-3'
					: i === cursor
						? 'cursor-pointer bg-sunken text-ink'
						: 'cursor-pointer text-ink'}"
			>
				<span class="grid size-4 shrink-0 place-items-center text-brand">
					{#if o.value === value}<Icon name="check" size={16} />{/if}
				</span>
				<span class="min-w-0 flex-1">
					<span class="block truncate font-medium">{o.label}</span>
					{#if o.hint}<span class="block truncate text-xs text-ink-3">{o.hint}</span>{/if}
				</span>
			</div>
		{/each}
	</div>
{/if}
