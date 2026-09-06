<!-- Logs tab: live container log tail over SSE with filters and pause.
     Access-log JSON lines are folded into a readable row; the raw record
     stays reachable through the title attribute. -->
<script lang="ts">
	import { api } from '$lib/api/client';
	import { formatTime } from '$lib/format';
	import type { LogLine } from '$lib/types/site';
	import Button from '$lib/components/Button.svelte';
	import Lamp from '$lib/components/Lamp.svelte';
	import Segmented from '$lib/components/Segmented.svelte';

	let { siteId }: { siteId: string } = $props();

	const MAX = 2000;
	type Filter = 'all' | 'stderr' | 'access';
	const filters: { value: Filter; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'stderr', label: 'Errors and PHP output' },
		{ value: 'access', label: 'Access log' }
	];

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

	type Access = {
		status: number;
		method: string;
		target: string;
		ms: number | null;
		cache: string | null;
	};

	/** Folds a Caddy access-log record into the few fields worth reading. */
	function parseAccess(text: string): Access | null {
		try {
			const rec = JSON.parse(text);
			const r = rec.request ?? {};
			const status = Number(rec.status ?? 0);
			const dur = typeof rec.duration === 'number' ? rec.duration * 1000 : null;
			const cs = rec.resp_headers?.['Cache-Status'];
			let cache: string | null = null;
			if (Array.isArray(cs) && cs.length) {
				const m = /\b(hit|miss|bypass|stale)\b/i.exec(String(cs[0]));
				cache = m ? m[1].toLowerCase() : null;
			}
			return {
				status,
				method: String(r.method ?? ''),
				target: `${r.host ?? ''}${r.uri ?? ''}`,
				ms: dur,
				cache
			};
		} catch {
			return null;
		}
	}

	function statusClass(status: number): string {
		if (status >= 500) return 'text-console-err';
		if (status >= 400) return 'text-console-warn';
		if (status >= 300) return 'text-info';
		return 'text-console-ink';
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

	const connState = $derived(ended ? 'stopped' : connected ? 'running' : 'starting');
	const connLabel = $derived(ended ? 'Stream ended' : connected ? 'Streaming' : 'Connecting');
</script>

<div class="flex flex-col gap-3">
	<div class="flex flex-wrap items-center gap-2">
		<Segmented bind:value={filter} options={filters} />
		<Button variant="ghost" icon={paused ? 'play' : 'pause'} onclick={togglePause}>
			{paused ? 'Resume' : 'Pause'}
		</Button>
		<Button variant="ghost" icon="broom" onclick={() => (lines = [])}>Clear</Button>
		<span class="ml-auto inline-flex items-center gap-2 text-xs text-ink-2 num">
			<Lamp status={connState} size={7} />
			{connLabel}, {visible.length} lines{paused && buffer.length
				? `, ${buffer.length} waiting`
				: ''}
		</span>
	</div>

	<div bind:this={scroller} class="h-[36rem] overflow-auto console p-4">
		{#if visible.length === 0}
			<p class="text-console-dim">
				{ended ? 'The container stopped writing logs.' : 'Waiting for log lines.'}
			</p>
		{:else}
			{#each visible as l, i (i)}
				{@const access = isAccess(l) ? parseAccess(l.text) : null}
				<div class="flex gap-3 break-all whitespace-pre-wrap">
					<span class="shrink-0 text-console-dim num select-none">{formatTime(l.time)}</span>
					{#if access}
						<span class="flex min-w-0 flex-wrap gap-x-3" title={l.text}>
							<span class="w-8 shrink-0 num {statusClass(access.status)}">{access.status}</span>
							<span class="w-14 shrink-0 text-console-dim">{access.method}</span>
							<span class="min-w-0 break-all text-console-ink">{access.target}</span>
							{#if access.ms !== null}
								<span class="shrink-0 text-console-dim num">{Math.round(access.ms)} ms</span>
							{/if}
							{#if access.cache}
								<span class="shrink-0 {access.cache === 'hit' ? 'text-ok' : 'text-console-dim'}">
									{access.cache}
								</span>
							{/if}
						</span>
					{:else}
						<span class={l.stream === 'stderr' ? 'text-console-warn' : 'text-console-ink'}>
							{l.text}
						</span>
					{/if}
				</div>
			{/each}
		{/if}
	</div>
</div>
