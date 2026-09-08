<!-- The text field. One 44px (or 36px) frame that holds the bare <input>
     and whatever sits beside it: a leading icon, a literal prefix ("wp"), a
     unit suffix, or the reveal toggle a password gets on its own. Every
     text-like control in the UI is this component, so focus, error and
     disabled read the same everywhere. Extra attributes (autocomplete, role,
     aria-*, onkeydown) fall through to the <input>. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	let {
		value = $bindable(''),
		type = 'text',
		id,
		size = 'md',
		mono = false,
		invalid = false,
		disabled = false,
		readonly = false,
		icon,
		prefix,
		suffix,
		revealable,
		shown = $bindable(false),
		class: klass = '',
		trailing,
		...rest
	}: {
		value?: string;
		type?: 'text' | 'email' | 'password' | 'search' | 'url';
		id?: string;
		size?: 'sm' | 'md';
		/** Code-shaped values: slugs, sizes like "256M", commands. */
		mono?: boolean;
		invalid?: boolean;
		disabled?: boolean;
		readonly?: boolean;
		/** Leading icon, for search-like fields. */
		icon?: IconName;
		/** Literal text before the value, e.g. "wp" or "https://". */
		prefix?: string;
		/** Literal text after the value, e.g. a unit. */
		suffix?: string;
		/** Password fields get a show/hide toggle unless this is false. */
		revealable?: boolean;
		/** Whether a password is currently shown; bind it to reveal from outside (a "Generate" button). */
		shown?: boolean;
		class?: string;
		/** Anything else that belongs inside the frame, after the value. */
		trailing?: Snippet;
		[key: string]: unknown;
	} = $props();

	const canReveal = $derived(type === 'password' && revealable !== false);
	const inputType = $derived(canReveal && shown ? 'text' : type);
</script>

<div
	class="flex items-center gap-2 border field-focus-within transition-[border-color,box-shadow] duration-150 {size ===
	'sm'
		? 'h-9 rounded-control-sm px-3'
		: 'h-11 rounded-control px-3.5'} {invalid
		? 'border-danger focus-within:border-danger focus-within:ring-danger/15'
		: 'border-rule-strong'} {disabled ? 'bg-sunken text-ink-3' : 'bg-surface text-ink'} {klass}"
>
	{#if icon}
		<Icon name={icon} size={size === 'sm' ? 16 : 18} class="text-ink-3" />
	{/if}
	{#if prefix}
		<span
			class="shrink-0 text-sm text-ink-3 select-none {mono ? 'font-mono' : ''}"
			aria-hidden="true">{prefix}</span
		>
	{/if}
	<input
		{id}
		type={inputType}
		bind:value
		{disabled}
		{readonly}
		aria-invalid={invalid || undefined}
		class="h-full min-w-0 flex-1 bg-transparent text-sm text-ink outline-none placeholder:text-ink-3 disabled:cursor-not-allowed disabled:text-ink-3 {mono
			? 'font-mono'
			: ''}"
		{...rest}
	/>
	{#if suffix}
		<span class="shrink-0 text-sm text-ink-3 select-none" aria-hidden="true">{suffix}</span>
	{/if}
	{#if trailing}
		{@render trailing()}
	{/if}
	{#if canReveal}
		<button
			type="button"
			class="-mr-1.5 grid size-8 shrink-0 place-items-center rounded-chip text-ink-3 transition-colors hover:bg-sunken hover:text-ink disabled:cursor-not-allowed disabled:opacity-50"
			onclick={() => (shown = !shown)}
			aria-label={shown ? 'Hide password' : 'Show password'}
			aria-pressed={shown}
			{disabled}
			tabindex="-1"
		>
			<Icon name={shown ? 'eye-off' : 'eye'} size={16} />
		</button>
	{/if}
</div>
