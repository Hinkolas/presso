<!-- Colored pill for site, job, health and component states. -->
<script lang="ts">
	let { status, label, dot = true }: { status: string; label?: string; dot?: boolean } = $props();

	const tones: Record<string, string> = {
		running: 'bg-green-50 text-green-700 ring-green-600/20',
		healthy: 'bg-green-50 text-green-700 ring-green-600/20',
		succeeded: 'bg-green-50 text-green-700 ring-green-600/20',
		ok: 'bg-green-50 text-green-700 ring-green-600/20',
		provisioning: 'bg-blue-50 text-blue-700 ring-blue-600/20',
		queued: 'bg-gray-100 text-gray-700 ring-gray-500/20',
		starting: 'bg-blue-50 text-blue-700 ring-blue-600/20',
		stopped: 'bg-gray-100 text-gray-600 ring-gray-500/20',
		exited: 'bg-gray-100 text-gray-600 ring-gray-500/20',
		none: 'bg-gray-100 text-gray-600 ring-gray-500/20',
		unknown: 'bg-gray-100 text-gray-600 ring-gray-500/20',
		error: 'bg-red-50 text-red-700 ring-red-600/20',
		failed: 'bg-red-50 text-red-700 ring-red-600/20',
		unhealthy: 'bg-red-50 text-red-700 ring-red-600/20',
		deleting: 'bg-amber-50 text-amber-700 ring-amber-600/20',
		missing: 'bg-amber-50 text-amber-700 ring-amber-600/20'
	};
	const dots: Record<string, string> = {
		running: 'bg-green-500',
		healthy: 'bg-green-500',
		succeeded: 'bg-green-500',
		ok: 'bg-green-500',
		provisioning: 'bg-blue-500 animate-pulse',
		starting: 'bg-blue-500 animate-pulse',
		queued: 'bg-gray-400',
		stopped: 'bg-gray-400',
		exited: 'bg-gray-400',
		none: 'bg-gray-400',
		unknown: 'bg-gray-400',
		error: 'bg-red-500',
		failed: 'bg-red-500',
		unhealthy: 'bg-red-500',
		deleting: 'bg-amber-500 animate-pulse',
		missing: 'bg-amber-500'
	};
	const tone = $derived(tones[status] ?? 'bg-gray-100 text-gray-700 ring-gray-500/20');
	const dotCls = $derived(dots[status] ?? 'bg-gray-400');
</script>

<span
	class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset {tone}"
>
	{#if dot}
		<span class="size-1.5 rounded-full {dotCls}"></span>
	{/if}
	{label ?? status}
</span>
