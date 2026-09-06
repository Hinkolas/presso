<!-- Button with the app's few visual variants. Renders an <a> when href is set. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import { resolve } from '$app/paths';
	import type { Pathname } from '$app/types';

	type Variant = 'primary' | 'secondary' | 'danger' | 'ghost';
	type Size = 'sm' | 'md';

	let {
		variant = 'secondary',
		size = 'md',
		href,
		busy = false,
		disabled = false,
		class: klass = '',
		children,
		...rest
	}: {
		variant?: Variant;
		size?: Size;
		/** Internal pathname; resolved against the app base. */
		href?: Pathname;
		busy?: boolean;
		disabled?: boolean;
		class?: string;
		children: Snippet;
	} & HTMLButtonAttributes = $props();

	const variants: Record<Variant, string> = {
		primary: 'bg-gray-900 text-white hover:bg-gray-700 border-transparent',
		secondary: 'bg-white text-gray-900 hover:bg-gray-50 border-gray-300',
		danger: 'bg-white text-red-700 hover:bg-red-50 border-red-300',
		ghost: 'bg-transparent text-gray-700 hover:bg-gray-100 border-transparent'
	};
	const sizes: Record<Size, string> = {
		sm: 'px-2.5 py-1 text-xs',
		md: 'px-3 py-1.5 text-sm'
	};
	const base =
		'inline-flex items-center justify-center gap-1.5 rounded-md border font-medium whitespace-nowrap transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-gray-400 disabled:cursor-not-allowed disabled:opacity-50';
	const cls = $derived(`${base} ${variants[variant]} ${sizes[size]} ${klass}`);
</script>

{#if href}
	<a href={resolve(href)} class={cls} aria-disabled={disabled || busy}>
		{@render children()}
	</a>
{:else}
	<button type="button" class={cls} disabled={disabled || busy} {...rest}>
		{#if busy}
			<span
				class="inline-block size-3 animate-spin rounded-full border-2 border-current border-t-transparent"
			></span>
		{/if}
		{@render children()}
	</button>
{/if}
