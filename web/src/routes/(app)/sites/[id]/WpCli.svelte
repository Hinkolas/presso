<!-- WP-CLI tab (admins only): run a command in the site container and show
     its output; the last ten commands are kept in memory. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { formatDuration } from '$lib/format';
	import { splitArgs } from '$lib/wp-args';
	import type { WpCliResponse } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';

	let { siteId, running }: { siteId: string; running: boolean } = $props();

	let input = $state('');
	let busy = $state(false);
	let error = $state('');
	let result = $state<WpCliResponse | null>(null);
	let history = $state<string[]>([]);

	async function run(e?: SubmitEvent) {
		e?.preventDefault();
		const args = splitArgs(input);
		if (args.length === 0) return;
		busy = true;
		error = '';
		try {
			result = await api.post<WpCliResponse>(`/v1/sites/${siteId}/wp`, { args });
			history = [input, ...history.filter((h) => h !== input)].slice(0, 10);
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="flex flex-col gap-4">
	{#if !running}
		<Alert tone="warn">The site is not running; commands will fail until it is.</Alert>
	{/if}
	<form onsubmit={run} class="flex items-center gap-2">
		<span class="font-mono text-sm text-gray-500">wp</span>
		<input
			class="input font-mono"
			bind:value={input}
			placeholder="plugin list --status=active"
			autocomplete="off"
			spellcheck="false"
		/>
		<Button type="submit" variant="primary" {busy} disabled={!input.trim()}>Run</Button>
	</form>

	{#if error}
		<Alert>{error}</Alert>
	{/if}

	{#if result}
		<div class="flex flex-wrap items-center gap-3 text-xs text-gray-600">
			<span
				class="rounded px-2 py-0.5 font-medium {result.exit_code === 0
					? 'bg-green-50 text-green-700'
					: 'bg-red-50 text-red-700'}"
			>
				exit {result.exit_code}
			</span>
			<span>{formatDuration(result.duration_ms)}</span>
			{#if result.truncated}
				<span class="text-amber-700">output truncated</span>
			{/if}
		</div>
		<div class="grid gap-3 lg:grid-cols-2">
			<div>
				<p class="mb-1 text-xs font-medium text-gray-500">stdout</p>
				<pre
					class="max-h-96 overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-5 break-all whitespace-pre-wrap text-gray-200">{result.stdout ||
						' '}</pre>
			</div>
			<div>
				<p class="mb-1 text-xs font-medium text-gray-500">stderr</p>
				<pre
					class="max-h-96 overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-3 font-mono text-xs leading-5 break-all whitespace-pre-wrap text-amber-200">{result.stderr ||
						' '}</pre>
			</div>
		</div>
	{/if}

	{#if history.length}
		<div>
			<p class="mb-1 text-xs font-medium text-gray-500">History</p>
			<ul class="flex flex-col gap-1">
				{#each history as h (h)}
					<li>
						<button
							type="button"
							onclick={() => (input = h)}
							class="rounded px-2 py-0.5 font-mono text-xs text-gray-700 hover:bg-gray-100"
						>
							wp {h}
						</button>
					</li>
				{/each}
			</ul>
		</div>
	{/if}
</div>
