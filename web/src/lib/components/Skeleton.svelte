<!-- Loading placeholder. Size it with `class`; `plate` rounds it like a
     plate for whole-panel stand-ins. Callers mark the region aria-busy. -->
<script lang="ts">
	let { class: klass = '', plate = false }: { class?: string; plate?: boolean } = $props();
</script>

<div
	class="animate-pulse bg-sunken {plate ? 'rounded-plate' : 'rounded-chip'} {klass}"
	aria-hidden="true"
></div>
