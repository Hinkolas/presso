<script lang="ts">
	import type { LayoutData } from './$types';

	let { data, children }: { data: LayoutData; children: import('svelte').Snippet } = $props();
</script>

<!-- Placeholder shell; the real navigation arrives with the design. -->
<div class="flex min-h-screen flex-col">
	<header class="flex items-center justify-between border-b px-6 py-3">
		<span class="font-semibold">presso</span>
		<form method="post" action="/auth/logout" class="flex items-center gap-3">
			<span class="text-sm opacity-70">{data.user.email}</span>
			<button type="submit" class="rounded border px-2 py-1 text-sm">Sign out</button>
		</form>
	</header>
	<div class="flex-1 p-6">
		{@render children()}
	</div>
</div>
