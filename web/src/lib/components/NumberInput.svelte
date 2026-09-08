<!-- A number with its unit and a stepper. The value is a real number the
     whole time the caller sees it; the text the user is typing is held here
     until it parses. Arrow keys step, the two chevrons step, blur snaps to
     the bounds. `suffix` is the unit ("MB", "s"); leave it off for counts. -->
<script lang="ts">
	let {
		value = $bindable(0),
		id,
		min,
		max,
		step = 1,
		suffix,
		invalid = false,
		disabled = false,
		class: klass = '',
		...rest
	}: {
		value?: number;
		id?: string;
		min?: number;
		max?: number;
		step?: number;
		suffix?: string;
		invalid?: boolean;
		disabled?: boolean;
		class?: string;
		[key: string]: unknown;
	} = $props();

	// Decimals the step needs, so 0.25 steps never show 0.30000000000000004.
	const decimals = $derived(Math.max(0, (String(step).split('.')[1] ?? '').length));
	// Fixed decimals, then trailing zeros after the point dropped ("1.50" -> "1.5").
	const fmt = (n: number) =>
		Number.isFinite(n)
			? decimals
				? n
						.toFixed(decimals)
						.replace(/(\.\d*?)0+$/, '$1')
						.replace(/\.$/, '')
				: n.toFixed(0)
			: '';

	let text = $state('');
	let editing = $state(false);

	// Follow the bound value while the field is not being typed in.
	$effect(() => {
		const v = value;
		if (!editing && Number(text) !== v) text = fmt(v);
	});

	function clamp(n: number): number {
		if (min !== undefined && n < min) n = min;
		if (max !== undefined && n > max) n = max;
		return Number(n.toFixed(decimals));
	}

	function onInput(e: Event) {
		text = (e.currentTarget as HTMLInputElement).value;
		const n = Number(text.replace(',', '.'));
		if (text.trim() !== '' && Number.isFinite(n)) value = clamp(n);
	}
	function onBlur() {
		editing = false;
		text = fmt(value);
	}
	function nudge(dir: 1 | -1) {
		if (disabled) return;
		const base = Number.isFinite(value) ? value : (min ?? 0);
		value = clamp(Math.round((base + dir * step) / step) * step);
		text = fmt(value);
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === 'ArrowUp') {
			e.preventDefault();
			nudge(1);
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			nudge(-1);
		}
	}

	const atMin = $derived(min !== undefined && value <= min);
	const atMax = $derived(max !== undefined && value >= max);
</script>

<div
	class="flex h-11 items-stretch overflow-hidden rounded-control border field-focus-within transition-[border-color,box-shadow] duration-150 {invalid
		? 'border-danger focus-within:border-danger focus-within:ring-danger/15'
		: 'border-rule-strong'} {disabled ? 'bg-sunken text-ink-3' : 'bg-surface text-ink'} {klass}"
>
	<input
		{id}
		type="text"
		inputmode="decimal"
		autocomplete="off"
		spellcheck="false"
		value={text}
		{disabled}
		role="spinbutton"
		aria-valuenow={value}
		aria-valuemin={min}
		aria-valuemax={max}
		aria-invalid={invalid || undefined}
		onfocus={() => (editing = true)}
		oninput={onInput}
		onblur={onBlur}
		onkeydown={onKey}
		class="min-w-0 flex-1 bg-transparent pl-3.5 font-mono text-sm text-ink num outline-none placeholder:text-ink-3 disabled:cursor-not-allowed disabled:text-ink-3 {suffix
			? 'pr-2'
			: 'pr-3'}"
		{...rest}
	/>
	{#if suffix}
		<span class="flex shrink-0 items-center pr-3 text-sm text-ink-3 select-none" aria-hidden="true">
			{suffix}
		</span>
	{/if}
	<div class="flex w-8 shrink-0 flex-col border-l border-rule">
		<button
			type="button"
			tabindex="-1"
			aria-label="Increase"
			class="grid flex-1 place-items-center text-ink-3 transition-colors hover:bg-sunken hover:text-ink disabled:pointer-events-none disabled:opacity-40"
			disabled={disabled || atMax}
			onclick={() => nudge(1)}
		>
			<svg
				width="10"
				height="10"
				viewBox="0 0 10 10"
				fill="none"
				stroke="currentColor"
				stroke-width="1.5"
				stroke-linecap="round"
				stroke-linejoin="round"
			>
				<path d="M2 6.5 5 3.5l3 3" />
			</svg>
		</button>
		<span class="h-px bg-rule"></span>
		<button
			type="button"
			tabindex="-1"
			aria-label="Decrease"
			class="grid flex-1 place-items-center text-ink-3 transition-colors hover:bg-sunken hover:text-ink disabled:pointer-events-none disabled:opacity-40"
			disabled={disabled || atMin}
			onclick={() => nudge(-1)}
		>
			<svg
				width="10"
				height="10"
				viewBox="0 0 10 10"
				fill="none"
				stroke="currentColor"
				stroke-width="1.5"
				stroke-linecap="round"
				stroke-linejoin="round"
			>
				<path d="M2 3.5 5 6.5l3-3" />
			</svg>
		</button>
	</div>
</div>
