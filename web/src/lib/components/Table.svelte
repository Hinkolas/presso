<!-- Table chrome: a plate that scrolls horizontally. Pass <thead>/<tbody> as
     children and use the exported cell classes. -->
<script lang="ts" module>
	export const th =
		'h-12 px-6 text-left text-xs font-medium text-ink-2 whitespace-nowrap bg-sunken/60';
	export const td = 'px-6 py-4 text-sm text-ink align-middle';
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	let { children, class: klass = '' }: { children: Snippet; class?: string } = $props();
</script>

<div class="overflow-x-auto frame {klass}">
	<table
		class="w-full min-w-[40rem] text-sm [&_tbody_tr]:border-t [&_tbody_tr]:border-rule [&_thead_th:first-child]:rounded-tl-plate [&_thead_th:last-child]:rounded-tr-plate"
	>
		{@render children()}
	</table>
</div>
