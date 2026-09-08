<!-- A site's front-page preview, or a placeholder with its initial. The
     image is the engine's stored capture; the version query busts the
     browser cache whenever a new capture lands. -->
<script lang="ts">
	import type { Site } from '$lib/types/site';
	import { screenshotUrl } from '$lib/site-actions';

	let {
		site,
		size = 'lg',
		class: klass = ''
	}: { site: Site; size?: 'sm' | 'lg'; class?: string } = $props();

	const shot = $derived(screenshotUrl(site));
	const dimmed = $derived(site.status !== 'running');
	const placeholder = $derived.by(() => {
		switch (site.status) {
			case 'provisioning':
				return 'The preview arrives once the site is up.';
			case 'error':
				return 'No preview while the site is failing.';
			default:
				return 'No preview captured yet.';
		}
	});
</script>

<div class="relative overflow-hidden bg-sunken {klass}">
	{#if shot}
		<img
			src={shot}
			alt="Front page of {site.name}"
			loading="lazy"
			class="h-full w-full object-cover object-top transition-[filter,opacity] duration-300 {dimmed
				? 'opacity-70 grayscale'
				: ''}"
		/>
	{:else}
		<div class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
			<span
				class="display leading-none text-rule-strong select-none {size === 'lg'
					? 'text-[72px]'
					: 'text-[30px]'}"
			>
				{site.name.trim().charAt(0).toUpperCase() || '?'}
			</span>
			{#if size === 'lg'}
				<span class="text-sm text-ink-3">{placeholder}</span>
			{/if}
		</div>
	{/if}
</div>
