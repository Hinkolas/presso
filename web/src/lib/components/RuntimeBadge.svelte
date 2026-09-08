<!-- Marks a site on the legacy runtime wherever the site is shown. The
     default runtime renders nothing unless `always` asks for a neutral
     "FrankenPHP" badge (comparison contexts like the version picker). -->
<script lang="ts">
	import type { RuntimeId } from '$lib/types/system';
	import Badge from './Badge.svelte';

	let {
		runtime,
		always = false,
		size = 'sm',
		class: klass = ''
	}: {
		runtime: RuntimeId | string | undefined;
		always?: boolean;
		size?: 'sm' | 'md';
		class?: string;
	} = $props();
</script>

{#if runtime === 'legacy'}
	<Badge tone="info" {size} title="Legacy runtime: nginx + PHP-FPM" class={klass}>Legacy</Badge>
{:else if always}
	<Badge tone="neutral" {size} title="FrankenPHP runtime: Caddy + PHP in one process" class={klass}>
		FrankenPHP
	</Badge>
{/if}
