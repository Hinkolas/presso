<!-- Page title row: serif headline, one line of meaning, actions to the right. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		title,
		subtitle,
		back,
		actions,
		children
	}: {
		title: string;
		subtitle?: string;
		back?: { href: string; label: string };
		actions?: Snippet;
		children?: Snippet;
	} = $props();
</script>

<header class="mb-8">
	{#if back}
		<!-- eslint-disable svelte/no-navigation-without-resolve -- callers pass resolved paths -->
		<a
			href={back.href}
			class="mb-4 inline-flex items-center gap-1 text-sm font-medium text-ink-2 hover:text-ink"
		>
			<Icon name="back" size={16} />
			{back.label}
		</a>
		<!-- eslint-enable svelte/no-navigation-without-resolve -->
	{/if}
	<div class="flex flex-wrap items-end justify-between gap-x-6 gap-y-4">
		<div class="min-w-0">
			<h1 class="display text-4xl text-ink">{title}</h1>
			{#if subtitle}
				<p class="mt-2 text-md text-ink-2">{subtitle}</p>
			{/if}
			{#if children}
				<div class="mt-3">{@render children()}</div>
			{/if}
		</div>
		{#if actions}
			<div class="flex flex-wrap items-center gap-3">{@render actions()}</div>
		{/if}
	</div>
</header>
