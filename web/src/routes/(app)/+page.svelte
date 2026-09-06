<script lang="ts">
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
</script>

<h1 class="text-2xl font-semibold">Sites</h1>
<p class="mt-2 opacity-70">
	Signed in as {data.user.name || data.user.email} ({data.user.role}). The site list lands here.
</p>
