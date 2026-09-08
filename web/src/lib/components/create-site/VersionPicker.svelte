<!-- WordPress version picker: a filter on top of an inline listbox (no
     popover inside the dialog). Each row says which PHP and runtime it lands
     on; rows the server cannot run are dimmed with the reason and skipped by
     the arrow keys. Enter picks the highlighted row and never submits the
     surrounding form; Escape only clears a non-empty filter. -->
<script lang="ts">
	import type { PickerRow } from '$lib/state/capabilities.svelte';
	import Badge from '../Badge.svelte';
	import Icon from '../Icon.svelte';
	import Input from '../Input.svelte';
	import RuntimeBadge from '../RuntimeBadge.svelte';

	let {
		rows,
		value = $bindable(''),
		defaultBranch = '',
		id = 'wp',
		invalid = false,
		onchange
	}: {
		rows: PickerRow[];
		/** The chosen branch. */
		value?: string;
		/** The server's default branch; marked "Recommended" when it is not the latest. */
		defaultBranch?: string;
		id?: string;
		invalid?: boolean;
		onchange?: (branch: string) => void;
	} = $props();

	let query = $state('');
	let cursor = $state(0);
	const listId = $derived(`${id}-list`);

	const filtered = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return rows;
		return rows.filter((r) =>
			`wordpress ${r.branch} ${r.version} ${r.status === 'latest' ? 'latest' : ''}`.includes(q)
		);
	});

	function step(from: number, dir: 1 | -1): number {
		for (let i = from + dir; i >= 0 && i < filtered.length; i += dir) {
			if (!filtered[i].disabled) return i;
		}
		return from;
	}
	function firstEnabled(): number {
		const i = filtered.findIndex((r) => !r.disabled);
		return i < 0 ? 0 : i;
	}
	function lastEnabled(): number {
		for (let i = filtered.length - 1; i >= 0; i--) if (!filtered[i].disabled) return i;
		return 0;
	}

	// The highlight follows the selection when the list changes; otherwise it
	// rests on the first row that can be chosen.
	$effect(() => {
		const i = filtered.findIndex((r) => r.branch === value && !r.disabled);
		cursor = i >= 0 ? i : firstEnabled();
	});
	$effect(() => {
		document.getElementById(`${listId}-${cursor}`)?.scrollIntoView({ block: 'nearest' });
	});

	function pick(row: PickerRow | undefined) {
		if (!row || row.disabled) return;
		if (row.branch !== value) {
			value = row.branch;
			onchange?.(row.branch);
		}
	}

	function onKey(e: KeyboardEvent) {
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
				// Picks, and never advances the step the picker sits in.
				e.preventDefault();
				pick(filtered[cursor]);
				break;
			case 'Escape':
				if (query) {
					e.preventDefault();
					e.stopPropagation();
					query = '';
				}
				break;
		}
	}
</script>

<div class="flex flex-col gap-2">
	<Input
		{id}
		type="search"
		icon="search"
		bind:value={query}
		onkeydown={onKey}
		{invalid}
		role="combobox"
		aria-expanded="true"
		aria-controls={listId}
		aria-activedescendant="{listId}-{cursor}"
		aria-autocomplete="list"
		aria-describedby={invalid ? `${id}-error` : undefined}
		placeholder="Filter versions, e.g. 6.4"
		autocomplete="off"
		spellcheck="false"
	/>
	<ul
		id={listId}
		role="listbox"
		aria-label="WordPress versions"
		class="max-h-72 overflow-y-auto rounded-plate border border-rule p-1.5"
	>
		{#each filtered as r, i (r.branch)}
			{@const selected = r.branch === value}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<li
				id="{listId}-{i}"
				role="option"
				aria-selected={selected}
				aria-disabled={r.disabled || undefined}
				onpointerenter={() => {
					if (!r.disabled) cursor = i;
				}}
				onclick={() => pick(r)}
				class="flex items-center gap-3 rounded-control-sm px-3 py-2.5 text-sm {r.disabled
					? 'cursor-not-allowed opacity-50'
					: i === cursor
						? 'bg-sunken'
						: ''}"
			>
				<span class="grid size-4 shrink-0 place-items-center text-brand">
					{#if selected}<Icon name="check" size={16} />{/if}
				</span>
				<span class="flex min-w-0 flex-1 flex-wrap items-center gap-x-2.5 gap-y-1">
					<span class="font-medium text-ink">WordPress {r.branch}</span>
					{#if r.version && r.version !== r.branch}
						<span class="text-xs text-ink-3 num">{r.version}</span>
					{/if}
					{#if r.status === 'latest'}
						<Badge tone="brand">Latest</Badge>
					{:else if r.branch === defaultBranch && !r.disabled}
						<Badge tone="brand">Recommended</Badge>
					{:else if !r.disabled}
						<span class="text-xs text-ink-3">Outdated</span>
					{/if}
				</span>
				<span class="flex shrink-0 items-center gap-2 text-xs text-ink-3">
					{#if r.disabled}
						{r.reason}
					{:else}
						<span class="num"
							>PHP {r.default_php}{r.runtime === 'frankenphp' ? ' · FrankenPHP' : ''}</span
						>
						<RuntimeBadge runtime={r.runtime} />
					{/if}
				</span>
			</li>
		{:else}
			<li class="px-3 py-6 text-center text-sm text-ink-3">No version matches “{query}”.</li>
		{/each}
	</ul>
</div>
