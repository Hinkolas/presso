<!-- Table chrome: horizontal scroll container plus consistent header styling.
     Pass <thead>/<tbody> content as children; use the exported cell classes. -->
<script lang="ts" module>
	export const th = 'px-4 py-2 text-left text-xs font-medium uppercase tracking-wide text-gray-500';
	export const td = 'px-4 py-2.5 text-sm text-gray-800 align-middle';
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	let { children, class: klass = '' }: { children: Snippet; class?: string } = $props();
</script>

<div class="overflow-x-auto rounded-lg border border-gray-200 bg-white {klass}">
	<table class="w-full min-w-[40rem] divide-y divide-gray-200 text-sm">
		{@render children()}
	</table>
</div>
