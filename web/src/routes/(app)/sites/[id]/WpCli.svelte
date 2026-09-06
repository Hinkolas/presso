<!-- WP-CLI tab (admins only): run a command in the site container and show
     its output; the last ten commands are kept in memory. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { formatDuration } from '$lib/format';
	import { splitArgs } from '$lib/wp-args';
	import type { WpCliResponse } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';

	let { siteId, running }: { siteId: string; running: boolean } = $props();

	let input = $state('');
	let busy = $state(false);
	let error = $state('');
	let result = $state<WpCliResponse | null>(null);
	let history = $state<string[]>([]);

	const examples = ['plugin list --status=active', 'option get siteurl', 'cache flush'];

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

<div class="flex flex-col gap-5">
	{#if !running}
		<Alert tone="warn">The site is not running. Start it before running commands.</Alert>
	{/if}

	<form onsubmit={run} class="flex items-center gap-2">
		<label
			class="flex input flex-1 items-center gap-2 py-0 font-mono focus-within:border-brand focus-within:ring-3 focus-within:ring-brand/20"
		>
			<span class="text-ink-3 select-none" aria-hidden="true">wp</span>
			<input
				class="min-w-0 flex-1 bg-transparent font-mono text-sm text-ink outline-none placeholder:text-ink-3"
				bind:value={input}
				placeholder="plugin list --status=active"
				autocomplete="off"
				spellcheck="false"
				aria-label="WP-CLI arguments"
			/>
		</label>
		<Button type="submit" variant="primary" icon="terminal" {busy} disabled={!input.trim()}>
			Run
		</Button>
	</form>

	<p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-3">
		Try
		{#each examples as ex, i (ex)}
			<button
				type="button"
				onclick={() => (input = ex)}
				class="rounded-control bg-sunken px-1.5 py-0.5 font-mono text-ink-2 hover:text-ink"
			>
				wp {ex}
			</button>{i < examples.length - 1 ? ',' : ''}
		{/each}
	</p>

	{#if error}
		<Alert>{error}</Alert>
	{/if}

	{#if result}
		<div class="flex flex-col gap-3">
			<div class="flex flex-wrap items-center gap-3 text-xs text-ink-2">
				<StatusPill
					status={result.exit_code === 0 ? 'succeeded' : 'failed'}
					label="Exit {result.exit_code}"
					soft
				/>
				<span class="num">{formatDuration(result.duration_ms)}</span>
				{#if result.truncated}
					<span class="text-busy">Output was cut at 1 MiB.</span>
				{/if}
			</div>
			<div class="grid gap-4 lg:grid-cols-2">
				<div>
					<p class="mb-1.5 text-xs text-ink-2">Output</p>
					<pre
						class="max-h-96 overflow-auto console p-3 break-all whitespace-pre-wrap">{result.stdout ||
							' '}</pre>
				</div>
				<div>
					<p class="mb-1.5 text-xs text-ink-2">Errors and warnings</p>
					<pre
						class="max-h-96 overflow-auto console p-3 break-all whitespace-pre-wrap text-console-warn">{result.stderr ||
							' '}</pre>
				</div>
			</div>
		</div>
	{/if}

	{#if history.length}
		<div>
			<p class="mb-1.5 text-xs text-ink-2">Recent commands</p>
			<ul class="flex flex-col items-start gap-0.5">
				{#each history as h (h)}
					<li>
						<Button variant="ghost" size="sm" class="font-mono" onclick={() => (input = h)}>
							wp {h}
						</Button>
					</li>
				{/each}
			</ul>
		</div>
	{/if}
</div>
