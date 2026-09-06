<!-- Logs tab: live container log tail over SSE with filters and pause. -->
<script lang="ts">
	import { api } from '$lib/api/client';
	import { formatTime } from '$lib/format';
	import type { LogLine } from '$lib/types/site';
	import Button from '$lib/components/Button.svelte';

	let { siteId }: { siteId: string } = $props();

	const MAX = 2000;
	type Filter = 'all' | 'stderr' | 'access';

	let lines = $state<LogLine[]>([]);
	let filter = $state<Filter>('all');
	let paused = $state(false);
	let ended = $state(false);
	let connected = $state(false);
	let buffer = $state<LogLine[]>([]);

	function push(l: LogLine) {
		if (paused) {
			buffer.push(l);
			if (buffer.length > MAX) buffer = buffer.slice(-MAX);
			return;
		}
		lines.push(l);
		if (lines.length > MAX) lines.splice(0, lines.length - MAX);
	}

	$effect(() => {
		const id = siteId;
		lines = [];
		buffer = [];
		ended = false;
		const es = api.stream(`/v1/sites/${id}/logs?tail=200&follow=1`);
		es.onopen = () => (connected = true);
		es.onerror = () => (connected = false);
		es.addEventListener('line', (e) => {
			try {
				push(JSON.parse((e as MessageEvent).data) as LogLine);
			} catch {
				// skip malformed frame
			}
		});
		es.addEventListener('end', () => {
			ended = true;
			connected = false;
			es.close();
		});
		return () => es.close();
	});

	function isAccess(l: LogLine): boolean {
		return l.text.startsWith('{') && l.text.includes('"request"');
	}

	const visible = $derived.by(() => {
		switch (filter) {
			case 'stderr':
				return lines.filter((l) => l.stream === 'stderr');
			case 'access':
				return lines.filter(isAccess);
			default:
				return lines;
		}
	});

	function togglePause() {
		paused = !paused;
		if (!paused && buffer.length) {
			for (const l of buffer) lines.push(l);
			buffer = [];
			if (lines.length > MAX) lines.splice(0, lines.length - MAX);
		}
	}

	let scroller: HTMLDivElement | undefined = $state();
	$effect(() => {
		void visible.length;
		if (!paused && scroller) scroller.scrollTop = scroller.scrollHeight;
	});
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<div class="inline-flex rounded-md border border-gray-300 bg-white p-0.5 text-sm">
			{#each [['all', 'All'], ['stderr', 'stderr only'], ['access', 'Access log']] as [f, label] (f)}
				<button
					type="button"
					onclick={() => (filter = f as Filter)}
					class="rounded px-3 py-1 font-medium {filter === f
						? 'bg-gray-900 text-white'
						: 'text-gray-600 hover:bg-gray-100'}"
				>
					{label}
				</button>
			{/each}
		</div>
		<Button onclick={togglePause}>{paused ? 'Resume' : 'Pause'}</Button>
		<Button onclick={() => (lines = [])}>Clear</Button>
		<span class="ml-auto text-xs text-gray-500">
			{#if ended}
				stream ended
			{:else if connected}
				<span class="text-green-700">streaming</span>
			{:else}
				connecting…
			{/if}
			· {visible.length} lines{paused && buffer.length ? ` · ${buffer.length} buffered` : ''}
		</span>
	</div>

	<div
		bind:this={scroller}
		class="h-[32rem] overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-5 text-gray-200"
	>
		{#if visible.length === 0}
			<p class="text-gray-500">No log lines.</p>
		{:else}
			{#each visible as l, i (i)}
				<div class="flex gap-3 break-all whitespace-pre-wrap">
					<span class="shrink-0 text-gray-500 select-none">{formatTime(l.time)}</span>
					<span class={l.stream === 'stderr' ? 'text-amber-300' : ''}>{l.text}</span>
				</div>
			{/each}
		{/if}
	</div>
</div>
