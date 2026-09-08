<!-- Overview: what is happening now, the last hour, the configuration, and
     the lifecycle controls. A job under way shows in the page's toast, not
     here. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { sites as liveSites } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { runtimeLabel } from '$lib/state/capabilities.svelte';
	import {
		cacheRatio,
		formatBytes,
		formatDateTime,
		formatDuration,
		formatNumber,
		percent
	} from '$lib/format';
	import type { Job } from '$lib/types/job';
	import type { MetricsResponse, Site, SiteAction, SiteStats } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Checkbox from '$lib/components/Checkbox.svelte';
	import Input from '$lib/components/Input.svelte';
	import LineChart from '$lib/components/LineChart.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import Stat from '$lib/components/Stat.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';

	let { site, activeJob, onJob }: { site: Site; activeJob: Job | null; onJob: (j: Job) => void } =
		$props();

	let busy = $state<string | null>(null);
	let error = $state('');
	let notice = $state('');

	const admin = $derived(isAdmin());
	const locked = $derived(
		activeJob !== null || site.status === 'provisioning' || site.status === 'deleting'
	);
	const running = $derived(site.status === 'running');

	async function action(name: SiteAction) {
		busy = name;
		error = '';
		notice = '';
		try {
			const res = await api.post<{ job?: Job; ok?: boolean; screenshot_at?: string }>(
				`/v1/sites/${site.id}/actions/${name}`
			);
			if (res?.job) onJob(res.job);
			else if (name === 'purge-cache') notice = 'Page cache purged. The next visit rebuilds it.';
			else if (name === 'flush-object-cache') notice = 'Object cache flushed.';
			else if (name === 'screenshot') {
				notice = 'Preview refreshed.';
				// The list payload carries screenshot_at; nudge the live record so
				// the header and the wall pick up the new image without a reload.
				if (res?.screenshot_at)
					liveSites.set(site.id, { ...site, screenshot_at: res.screenshot_at });
			}
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = null;
		}
	}

	// Live container stats while running.
	let stats = $state<SiteStats | null>(null);
	$effect(() => {
		if (!running) {
			stats = null;
			return;
		}
		const id = site.id;
		let stop = false;
		const tick = async () => {
			try {
				const s = await api.get<SiteStats>(`/v1/sites/${id}/stats`);
				if (!stop) stats = s;
			} catch {
				// Leave the previous sample in place.
			}
		};
		tick();
		const t = setInterval(tick, 15_000);
		return () => {
			stop = true;
			clearInterval(t);
		};
	});

	// Last hour of request buckets for the small chart.
	let hour = $state<MetricsResponse | null>(null);
	$effect(() => {
		const id = site.id;
		const load = () =>
			api
				.get<MetricsResponse>(`/v1/sites/${id}/metrics?range=1h`)
				.then((r) => (hour = r))
				.catch(() => {});
		load();
		const t = setInterval(load, 60_000);
		return () => clearInterval(t);
	});
	const hourWindow = $derived.by<[number, number]>(() => {
		const end = Math.floor(Date.now() / 1000);
		void hour;
		return [end - 3600, end];
	});
	const hourTimes = $derived(
		(hour?.requests ?? []).map((b) => Math.floor(new Date(b.bucket).getTime() / 1000))
	);
	const hourSeries = $derived([
		(hour?.requests ?? []).map((b) => b.cache_hit + b.cache_stale),
		(hour?.requests ?? []).map((b) => b.cache_miss + b.cache_bypass)
	]);
	// Totals for the hour come from the buckets: the detail endpoint carries no
	// summary, and the list's summary would go stale between visits.
	const m = $derived.by(() => {
		const buckets = hour?.requests ?? [];
		if (!buckets.length) return site.metrics_1h;
		const sum = (f: (b: (typeof buckets)[number]) => number) =>
			buckets.reduce((a, b) => a + f(b), 0);
		return {
			requests: sum((b) => b.requests),
			cache_hit: sum((b) => b.cache_hit),
			cache_miss: sum((b) => b.cache_miss),
			cache_bypass: sum((b) => b.cache_bypass),
			cache_stale: sum((b) => b.cache_stale),
			status_5xx: sum((b) => b.status_5xx)
		};
	});
	const p95 = $derived.by(() => {
		const buckets = (hour?.requests ?? []).filter((b) => b.requests > 0);
		if (!buckets.length) return null;
		return Math.max(...buckets.map((b) => b.dur_p95_ms));
	});

	let deleteOpen = $state(false);
	let confirmSlug = $state('');
	let keepVolume = $state(false);
	let deleteError = $state('');

	async function confirmDelete() {
		busy = 'delete';
		deleteError = '';
		try {
			const res = await api.del<{ job: Job }>(`/v1/sites/${site.id}`, {
				confirm: confirmSlug,
				keep_volume: keepVolume
			});
			onJob(res.job);
			deleteOpen = false;
			// The card on the Sites page shows the teardown and then goes away.
			await goto(resolve('/'));
		} catch (err) {
			deleteError = errorText(err);
		} finally {
			busy = null;
		}
	}

	function ttlText(seconds: number): string {
		if (seconds % 3600 === 0) return `${seconds / 3600} h`;
		if (seconds % 60 === 0) return `${seconds / 60} min`;
		return formatDuration(seconds * 1000);
	}

	const config: [string, string][] = $derived([
		['WordPress', site.wp_version || '–'],
		['PHP', site.php_version],
		['Runtime', runtimeLabel(site.runtime)],
		['Target image', site.image || '–'],
		['Last applied image', site.applied_image || 'Not yet applied'],
		['Profile', site.profile === 'dev' ? 'Development' : 'Production'],
		['Plugin and theme installs', site.file_mods === 'locked' ? 'Locked' : 'Allowed'],
		[
			'Page cache',
			site.page_cache
				? [
						`On, kept up to ${ttlText(site.settings.cache_ttl)}`,
						site.settings.cache_swr ? 'served stale while refreshing' : '',
						site.settings.cache_warm ? 'kept warm' : ''
					]
						.filter(Boolean)
						.join(', ')
				: 'Off'
		],
		['TLS', site.tls_mode === 'auto' ? 'Automatic certificate' : 'Off'],
		[
			'Container limits',
			`${site.settings.container_memory_mb} MB, ${site.settings.container_cpus} CPU`
		],
		['PHP memory', site.settings.php_memory_limit],
		['Upload limit', site.settings.upload_max_filesize],
		['Administrator', `${site.admin_user}, ${site.admin_email}`],
		['Created', formatDateTime(site.created_at)]
	]);
</script>

<div class="flex flex-col gap-5">
	{#if error}<Alert>{error}</Alert>{/if}
	{#if notice}<Alert tone="success">{notice}</Alert>{/if}

	<div class="grid gap-5 lg:grid-cols-3">
		<Panel title="Right now">
			<div class="grid grid-cols-2 gap-x-4 gap-y-5">
				<div class="flex flex-col">
					<span class="text-xs text-ink-2">Status</span>
					<span class="mt-1"><StatusPill status={site.status} soft /></span>
				</div>
				<div class="flex flex-col">
					<span class="text-xs text-ink-2">Container health</span>
					<span class="mt-1">
						{#if site.health && site.health !== 'none'}
							<StatusPill status={site.health} soft />
						{:else}
							<span class="text-sm text-ink-3">–</span>
						{/if}
					</span>
				</div>
				<Stat label="CPU" value={stats ? `${stats.cpu_percent.toFixed(1)}%` : '–'} />
				<Stat
					label="Memory"
					value={stats ? formatBytes(stats.mem_usage) : '–'}
					sub={stats?.mem_limit ? `of ${formatBytes(stats.mem_limit)}` : undefined}
				/>
			</div>
		</Panel>

		<Panel
			title="Last hour"
			subtitle="Visitor traffic; wp-admin is under Metrics"
			class="lg:col-span-2"
		>
			<div class="grid gap-5 sm:grid-cols-[10rem_minmax(0,1fr)]">
				<div class="flex flex-col gap-5">
					<Stat label="Requests" value={formatNumber(m?.requests ?? 0)} size="lg" />
					<Stat
						label="Served from cache"
						value={m && m.requests ? cacheRatio(m) : '–'}
						sub={m && m.cache_bypass
							? `${percent(m.cache_bypass, m.requests)} bypassed: logged in or POST`
							: undefined}
						tone={m && m.requests ? 'ok' : 'ink'}
					/>
					<Stat
						label="Slowest p95"
						value={p95 !== null ? formatDuration(p95) : '–'}
						sub={m?.status_5xx ? `${formatNumber(m.status_5xx)} server errors` : 'no server errors'}
						tone={m?.status_5xx ? 'danger' : 'ink'}
					/>
				</div>
				<div class="min-w-0">
					{#if hour && hour.requests.length}
						<LineChart
							times={hourTimes}
							values={hourSeries}
							height={168}
							xRange={hourWindow}
							series={[
								{ label: 'from cache', tone: 'ok', fill: true },
								{ label: 'to origin', tone: 'ink-3', fill: true }
							]}
							format={(v) => String(Math.round(v))}
						/>
					{:else}
						<div
							class="grid h-full min-h-40 place-items-center rounded-plate border border-dashed border-rule-strong text-sm text-ink-3"
						>
							No requests in the last hour.
						</div>
					{/if}
				</div>
			</div>
		</Panel>
	</div>

	<div class="grid gap-5 lg:grid-cols-3">
		<Panel title="Configuration" class="lg:col-span-2" padded={false}>
			<dl class="grid grid-cols-1 sm:grid-cols-2">
				{#each config as [label, value] (label)}
					<div
						class="flex flex-col gap-0.5 border-b border-rule px-6 py-3.5 last:border-b-0 sm:[&:nth-last-child(2)]:border-b-0"
					>
						<dt class="text-xs text-ink-2">{label}</dt>
						<dd class="truncate text-sm text-ink" title={value}>{value}</dd>
					</div>
				{/each}
			</dl>
			{#if site.domains.length > 1}
				<div class="border-t border-rule px-6 py-4">
					<p class="text-xs text-ink-2">Domains</p>
					<ul class="mt-1.5 flex flex-wrap gap-2">
						{#each site.domains as d (d.id)}
							<li>
								<Badge tone="neutral" size="md" class="font-mono font-medium">
									{d.host}{d.kind === 'primary' ? ' (primary)' : ''}
								</Badge>
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		</Panel>

		{#if admin}
			<Panel title="Controls">
				<div class="flex flex-col gap-2">
					{#if site.status === 'stopped' || site.status === 'error'}
						<Button
							icon="play"
							onclick={() => action('start')}
							busy={busy === 'start'}
							disabled={locked}>Start</Button
						>
					{/if}
					{#if running}
						<Button
							icon="stop"
							onclick={() => action('stop')}
							busy={busy === 'stop'}
							disabled={locked}>Stop</Button
						>
						<Button
							icon="refresh"
							onclick={() => action('restart')}
							busy={busy === 'restart'}
							disabled={locked}>Restart</Button
						>
					{/if}
					{#if site.status === 'error'}
						<Button
							icon="refresh"
							onclick={() => action('retry')}
							busy={busy === 'retry'}
							disabled={locked}>Retry</Button
						>
					{/if}
					<Button onclick={() => action('recreate')} busy={busy === 'recreate'} disabled={locked}
						>Recreate container</Button
					>
					<div class="my-1 h-px bg-rule"></div>
					<Button
						icon="broom"
						onclick={() => action('purge-cache')}
						busy={busy === 'purge-cache'}
						disabled={!running}>Purge page cache</Button
					>
					<Button
						onclick={() => action('flush-object-cache')}
						busy={busy === 'flush-object-cache'}
						disabled={!running}>Flush object cache</Button
					>
					<Button
						icon="camera"
						onclick={() => action('screenshot')}
						busy={busy === 'screenshot'}
						disabled={!running}>Refresh preview</Button
					>
					<div class="my-1 h-px bg-rule"></div>
					<Button
						variant="danger"
						icon="trash"
						onclick={() => {
							confirmSlug = '';
							keepVolume = false;
							deleteError = '';
							deleteOpen = true;
						}}
						disabled={site.status === 'deleting'}
					>
						Delete site
					</Button>
				</div>
			</Panel>
		{/if}
	</div>
</div>

<Modal bind:open={deleteOpen} title="Delete {site.name}">
	<p>
		The container, its route at the edge, the database and the cache user are removed. Type
		<code class="rounded bg-sunken px-1 text-ink">{site.slug}</code> to confirm.
	</p>
	<Input
		class="mt-3"
		mono
		bind:value={confirmSlug}
		placeholder={site.slug}
		autocomplete="off"
		spellcheck="false"
		aria-label="Type the slug to confirm"
	/>
	<div class="mt-4">
		<Checkbox
			bind:checked={keepVolume}
			label="Keep the files volume"
			hint="Uploads, plugins and themes stay on disk for later recovery."
		/>
	</div>
	{#if deleteError}
		<Alert class="mt-3">{deleteError}</Alert>
	{/if}
	{#snippet footer()}
		<Button onclick={() => (deleteOpen = false)}>Cancel</Button>
		<Button
			variant="danger"
			onclick={confirmDelete}
			busy={busy === 'delete'}
			disabled={confirmSlug !== site.slug}
		>
			Delete site
		</Button>
	{/snippet}
</Modal>
