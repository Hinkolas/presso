<!-- Lamp plus wording. `soft` renders the tinted 30px chip; the default is
     lamp and text only, for dense rows. -->
<script lang="ts">
	import Lamp from './Lamp.svelte';
	import { statusLabel, toneOf, toneSoft, toneText } from './status';

	let {
		status,
		label,
		soft = false,
		class: klass = ''
	}: { status: string; label?: string; soft?: boolean; class?: string } = $props();
	const tone = $derived(toneOf(status));
</script>

{#if soft}
	<span
		class="inline-flex h-[30px] items-center gap-2 rounded-full px-3 text-xs font-semibold whitespace-nowrap {toneSoft[
			tone
		]} {klass}"
	>
		<Lamp {status} size={8} />
		{label ?? statusLabel(status)}
	</span>
{:else}
	<span
		class="inline-flex items-center gap-2 text-sm font-medium whitespace-nowrap {toneText[
			tone
		]} {klass}"
	>
		<Lamp {status} size={8} />
		{label ?? statusLabel(status)}
	</span>
{/if}
