<!-- "Back to the list" above a detail page's title. -->
<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		href,
		label,
		class: klass = ''
	}: { href: string; label: string; class?: string } = $props();
</script>

<!-- eslint-disable svelte/no-navigation-without-resolve -- callers pass resolved paths -->
<a
	{href}
	class="-ml-2 inline-flex h-8 items-center gap-1 rounded-chip pr-2.5 pl-1.5 text-sm font-medium text-ink-2 transition-colors hover:bg-sunken hover:text-ink {klass}"
>
	<Icon name="back" size={16} />
	{label}
</a>
<!-- eslint-enable svelte/no-navigation-without-resolve -->
