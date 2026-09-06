<!-- The one button. `primary` is forest green with a soft shadow and is
     reserved for the main action of a view; `default` is the white control;
     `ghost` sits in toolbars; `danger` is red and only for destructive
     actions. `md` is 44px, `sm` 36px. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	let {
		variant = 'default',
		size = 'md',
		type = 'button',
		href,
		busy = false,
		disabled = false,
		icon,
		class: klass = '',
		onclick,
		children,
		...rest
	}: {
		variant?: 'primary' | 'default' | 'ghost' | 'danger';
		size?: 'sm' | 'md';
		type?: 'button' | 'submit';
		href?: string;
		busy?: boolean;
		disabled?: boolean;
		icon?: IconName;
		class?: string;
		onclick?: (e: MouseEvent) => void;
		children?: Snippet;
		[key: string]: unknown;
	} = $props();

	const base =
		'inline-flex items-center justify-center gap-2 font-semibold whitespace-nowrap select-none transition-[background-color,box-shadow,color] duration-150 disabled:cursor-not-allowed disabled:opacity-50';
	const sizes = {
		sm: 'h-9 rounded-control-sm px-3.5 text-sm',
		md: 'h-11 rounded-control px-[18px] text-sm'
	};
	const variants = {
		primary: 'bg-brand text-on-brand shadow-primary hover:bg-brand-hover',
		default: 'border border-rule-strong bg-surface text-ink hover:bg-sunken',
		ghost: 'text-ink-2 hover:bg-sunken hover:text-ink',
		danger: 'border border-danger/30 bg-surface text-danger-ink hover:bg-danger-soft'
	};
	const iconOnly = $derived(!children && !!icon);
	const cls = $derived(
		`${base} ${sizes[size]} ${variants[variant]} ${iconOnly ? (size === 'sm' ? 'w-9 px-0' : 'w-11 px-0') : ''} ${klass}`
	);
	const inert = $derived(disabled || busy);
</script>

{#if href && !inert}
	<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- callers pass resolved paths -->
	<a {href} class={cls} {...rest}>
		{#if icon}<Icon name={icon} size={size === 'sm' ? 16 : 18} />{/if}
		{#if children}{@render children()}{/if}
	</a>
{:else}
	<button {type} class={cls} disabled={inert} aria-busy={busy || undefined} {onclick} {...rest}>
		{#if busy}
			<span
				class="size-4 animate-spin rounded-full border-[1.5px] border-current border-t-transparent"
			></span>
		{:else if icon}
			<Icon name={icon} size={size === 'sm' ? 16 : 18} />
		{/if}
		{#if children}{@render children()}{/if}
	</button>
{/if}
