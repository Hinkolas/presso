<!-- Status dot. Breathes while the state is in motion. -->
<script lang="ts">
	import { toneBg, toneOf } from './status';

	let {
		status,
		size = 8,
		class: klass = ''
	}: { status: string; size?: number; class?: string } = $props();
	const tone = $derived(toneOf(status));
</script>

<span
	class="inline-block shrink-0 rounded-full {toneBg[tone]} {tone === 'busy'
		? 'breathe'
		: ''} {klass}"
	style="width:{size}px;height:{size}px"
	aria-hidden="true"
></span>
