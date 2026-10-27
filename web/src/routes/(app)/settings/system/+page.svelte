<!-- System: two tabs (?tab=). Overview is the shared pieces every site
     depends on, Docker, the edge, updates, and what the host is spending on
     disk, refreshed every 30 seconds (the component strip and the update
     state also follow live platform.status / system.update events). Activity
     is the job history. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobs, live } from '$lib/state/live.svelte';
	import { formatBytes, formatDateTime, formatRelative, formatUptime } from '$lib/format';
	import type { PlatformComponent, SystemStatus, UpdateStatus } from '$lib/types/system';
	import type { Job } from '$lib/types/job';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Lamp from '$lib/components/Lamp.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import StatusPill from '$lib/components/StatusPill.svelte';
	import Activity from './Activity.svelte';
	import { statusLabel } from '$lib/components/status';

	type Tab = 'overview' | 'activity';
	const tabs: { id: Tab; label: string }[] = [
		{ id: 'overview', label: 'Overview' },
		{ id: 'activity', label: 'Activity' }
	];
	const tab = $derived.by<Tab>(() =>
		page.url.searchParams.get('tab') === 'activity' ? 'activity' : 'overview'
	);
	function setTab(t: Tab) {
		const search = t === 'overview' ? '' : `?tab=${t}`;
		// eslint-disable-next-line svelte/no-navigation-without-resolve -- resolved path plus query
		goto(resolve('/system') + search, { replaceState: true, noScroll: true, keepFocus: true });
	}

	let status = $state<SystemStatus | null>(null);
	let error = $state('');

	async function load() {
		try {
			status = await api.get<SystemStatus>('/v1/system/status');
			error = '';
		} catch (err) {
			error = errorText(err);
		}
	}

	$effect(() => {
		load();
		const t = setInterval(load, 30_000);
		return () => clearInterval(t);
	});

	// --- Updates -----------------------------------------------------------
	// The live event wins over the polled snapshot; both carry the same shape.
	const update = $derived<UpdateStatus | null>(live.update ?? status?.update ?? null);
	// The engine job to follow: from the status block, else the freshest
	// unfinished engine.* job the stream knows.
	const engineJob = $derived.by<Job | null>(() => {
		if (update?.job_id) return jobs.get(update.job_id) ?? null;
		let found: Job | null = null;
		for (const j of jobs.values()) {
			if (!j.kind.startsWith('engine.') || (j.status !== 'queued' && j.status !== 'running'))
				continue;
			if (!found || j.created_at > found.created_at) found = j;
		}
		return found;
	});
	const engineBusy = $derived(!!update?.job_id || !!engineJob);

	let checking = $state(false);
	let updateError = $state('');
	async function checkNow() {
		checking = true;
		updateError = '';
		try {
			const r = await api.post<{ update: UpdateStatus }>('/v1/system/actions/check-update', {});
			live.update = r.update;
		} catch (err) {
			updateError = errorText(err);
		} finally {
			checking = false;
		}
	}

	let confirmOpen = $state(false);
	let starting = $state<'update' | 'rollout' | null>(null);
	async function run(action: 'update' | 'rollout') {
		starting = action;
		updateError = '';
		try {
			const r = await api.post<{ job: Job }>(`/v1/system/actions/${action}`, {});
			confirmOpen = false;
			await goto(resolve(`/jobs/${r.job.id}`));
		} catch (err) {
			updateError = errorText(err);
		} finally {
			starting = null;
		}
	}

	const restarting = $derived(live.restarting);
	function shortHost(url: string): string {
		try {
			return new URL(url).host;
		} catch {
			return url;
		}
	}

	const components = $derived(live.platform ?? status?.components ?? []);
	// The one-line verdict the sidebar used to carry; it belongs under the title.
	const health = $derived.by(() => {
		if (components.length === 0) return '';
		const bad = components.some(
			(c) => c.error || c.state === 'error' || c.state === 'missing' || c.health === 'unhealthy'
		);
		return bad ? 'A component needs attention' : 'All components healthy';
	});
	const siteCounts = $derived(Object.entries(status?.sites ?? {}).sort());

	const names: Record<string, string> = {
		network: 'Network',
		mariadb: 'MariaDB',
		valkey: 'Valkey',
		edge: 'Edge',
		browser: 'Preview browser'
	};
	function nameOf(c: PlatformComponent): string {
		return names[c.name] ?? c.name;
	}
	/** The lamp follows health when the container reports one, else state. */
	function lampStatus(c: PlatformComponent): string {
		if (c.error) return 'error';
		if (c.health && c.health !== 'none') return c.health;
		return c.state;
	}
	function stateText(c: PlatformComponent): string {
		const s = statusLabel(c.state);
		if (c.health && c.health !== 'none' && c.health !== c.state) {
			return `${s}, ${statusLabel(c.health).toLowerCase()}`;
		}
		return s;
	}

	const diskTotal = $derived(
		status
			? status.disk.data_dir_bytes +
					status.disk.images_bytes +
					status.disk.containers_bytes +
					status.disk.volumes_bytes
			: 0
	);
</script>

<svelte:head>
	<title>{tab === 'activity' ? 'Activity' : 'System'} · presso</title>
</svelte:head>

<PageHeader
	title="System"
	subtitle={status
		? [health, `presso ${status.version}, up ${formatUptime(status.uptime_seconds)}`]
				.filter(Boolean)
				.join(' · ')
		: 'Loading the host status.'}
/>

<nav
	class="mb-8 flex [scrollbar-width:none] gap-2 overflow-x-auto border-b border-rule"
	aria-label="System sections"
>
	{#each tabs as t (t.id)}
		<button
			type="button"
			onclick={() => setTab(t.id)}
			class="group relative -mb-px h-12 px-1 text-base font-medium transition-colors {tab === t.id
				? 'text-ink'
				: 'text-ink-2 hover:text-ink'}"
			aria-current={tab === t.id ? 'page' : undefined}
		>
			<span
				class="inline-flex h-9 items-center rounded-chip px-3 transition-colors group-hover:bg-sunken {tab ===
				t.id
					? 'group-hover:bg-transparent'
					: ''}"
			>
				{t.label}
			</span>
			{#if tab === t.id}
				<span class="absolute inset-x-4 bottom-0 h-0.5 rounded-t bg-brand"></span>
			{/if}
		</button>
	{/each}
</nav>

{#if tab === 'activity'}
	<Activity />
{:else}
	{#if error}
		<Alert class="mb-5">{error}</Alert>
	{/if}

	{#if !status}
		{#if !error}
			<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5" aria-busy="true">
				{#each [0, 1, 2, 3, 4] as i (i)}
					<Skeleton plate class="h-24" />
				{/each}
			</div>
		{/if}
	{:else}
		<div class="flex flex-col gap-5">
			<section aria-label="Platform components" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
				{#each components as c (c.name)}
					<div class="flex flex-col gap-2 frame px-5 py-4">
						<div class="flex items-center gap-2">
							<Lamp status={lampStatus(c)} />
							<span class="text-sm font-medium text-ink">{nameOf(c)}</span>
						</div>
						<span class="text-xs text-ink-2">{stateText(c)}</span>
						{#if c.image}
							<span class="truncate font-mono text-2xs text-ink-3" title={c.image}>{c.image}</span>
						{/if}
						{#if c.error}
							<span class="text-xs leading-4 text-danger">{c.error}</span>
						{/if}
					</div>
				{:else}
					<p class="text-sm text-ink-3 sm:col-span-2 lg:col-span-5">
						No components reported yet. The reconciler runs on boot and every minute.
					</p>
				{/each}
			</section>

			{#if update}
				<Panel title="Updates">
					{#if restarting}
						<Alert class="mb-4">
							presso is restarting into {restarting}. The app reloads on its own once the engine and
							the edge are back.
						</Alert>
					{/if}
					<div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
						<dl class="grid grid-cols-[8rem_minmax(0,1fr)] gap-y-2 text-sm">
							<dt class="text-ink-2">Installed</dt>
							<dd class="text-ink num">{update.current}</dd>
							<dt class="text-ink-2">Latest release</dt>
							<dd class="text-ink">
								{#if update.latest}
									<span class="num">{update.latest}</span>
									{#if update.available}
										<span
											class="ml-2 rounded-chip bg-brand-soft px-2 py-0.5 text-xs font-medium text-ink"
											>update available</span
										>
									{:else}
										<span class="ml-2 text-xs text-ink-3">you are up to date</span>
									{/if}
									{#if update.url}
										<!-- eslint-disable svelte/no-navigation-without-resolve -- external release URL -->
										<a
											href={update.url}
											target="_blank"
											rel="noreferrer"
											class="ml-2 text-xs text-ink-2 underline decoration-rule-strong hover:text-ink"
											>release notes on {shortHost(update.url)}</a
										>
										<!-- eslint-enable svelte/no-navigation-without-resolve -->
									{/if}
								{:else if update.error}
									<span class="text-ink-3">unknown</span>
								{:else}
									<span class="text-ink-3">not checked yet</span>
								{/if}
							</dd>
							<dt class="text-ink-2">Last check</dt>
							<dd class="text-ink" title={formatDateTime(update.checked_at)}>
								{update.checked_at ? formatRelative(update.checked_at) : 'Never'}
							</dd>
						</dl>
						<div class="flex flex-wrap gap-2">
							<Button size="sm" onclick={checkNow} busy={checking} disabled={engineBusy}
								>Check now</Button
							>
							<Button
								size="sm"
								variant="primary"
								onclick={() => (confirmOpen = true)}
								disabled={!update.available || !update.supported || engineBusy}
								title={!update.supported ? update.unsupported_reason : undefined}
							>
								{update.available ? `Update to ${update.latest}` : 'Update'}
							</Button>
							<Button
								size="sm"
								onclick={() => run('rollout')}
								busy={starting === 'rollout'}
								disabled={engineBusy}
								title="Move every site to the runtime images of the installed release"
							>
								Roll out runtime images
							</Button>
						</div>
					</div>
					{#if !update.supported && update.unsupported_reason}
						<p class="mt-3 text-xs text-ink-3">
							Self-update is unavailable here: {update.unsupported_reason}.
						</p>
					{/if}
					{#if update.error}
						<Alert class="mt-4">Update check failed: {update.error}</Alert>
					{/if}
					{#if updateError}
						<Alert class="mt-4">{updateError}</Alert>
					{/if}
					{#if engineJob && !restarting}
						<p class="mt-3 text-xs text-ink-2">
							{engineJob.kind === 'engine.update' ? 'Update' : 'Rollout'} in progress ·
							<a
								href={resolve(`/jobs/${engineJob.id}`)}
								class="underline decoration-rule-strong hover:text-ink">follow the job</a
							>
						</p>
					{/if}
				</Panel>
			{/if}

			<div class="grid gap-5 lg:grid-cols-2">
				<Panel title="Docker">
					<div class="mb-4">
						<StatusPill
							status={status.docker.ok ? 'ok' : 'error'}
							label={status.docker.ok ? 'Connected' : 'Unavailable'}
							soft
						/>
					</div>
					{#if status.docker.error}
						<Alert>{status.docker.error}</Alert>
					{:else}
						<dl class="grid grid-cols-[8rem_minmax(0,1fr)] gap-y-2 text-sm">
							<dt class="text-ink-2">Engine version</dt>
							<dd class="text-ink num">{status.docker.version || '–'}</dd>
							<dt class="text-ink-2">API version</dt>
							<dd class="text-ink num">{status.docker.api_version || '–'}</dd>
							<dt class="text-ink-2">Host</dt>
							<dd class="text-ink">{status.docker.os || '–'}, {status.docker.arch || '–'}</dd>
						</dl>
					{/if}
				</Panel>

				<Panel title="Edge">
					<dl class="grid grid-cols-[8rem_minmax(0,1fr)] gap-y-2 text-sm">
						<dt class="text-ink-2">Routes applied</dt>
						<dd class="text-ink" title={formatDateTime(status.edge.last_applied_at)}>
							{status.edge.last_applied_at ? formatRelative(status.edge.last_applied_at) : 'Never'}
						</dd>
						<dt class="text-ink-2">Routed sites</dt>
						<dd class="text-ink num">{status.edge.sites}</dd>
					</dl>
					{#if status.edge.last_error}
						<Alert class="mt-4">{status.edge.last_error}</Alert>
					{/if}
				</Panel>

				<Panel title="Sites">
					{#if siteCounts.length === 0}
						<p class="text-sm text-ink-3">No sites yet.</p>
					{:else}
						<ul class="flex flex-col gap-2.5">
							{#each siteCounts as [s, n] (s)}
								<li class="flex items-center justify-between gap-3 text-sm">
									<StatusPill status={s} />
									<span class="font-medium text-ink num">{n}</span>
								</li>
							{/each}
						</ul>
					{/if}
				</Panel>

				<Panel title="Disk">
					<dl class="grid grid-cols-[minmax(0,1fr)_auto] gap-y-2 text-sm">
						<dt class="text-ink-2">Engine data</dt>
						<dd class="text-ink num">{formatBytes(status.disk.data_dir_bytes)}</dd>
						<dt class="text-ink-2">Images</dt>
						<dd class="text-ink num">{formatBytes(status.disk.images_bytes)}</dd>
						<dt class="text-ink-2">Containers</dt>
						<dd class="text-ink num">{formatBytes(status.disk.containers_bytes)}</dd>
						<dt class="text-ink-2">Volumes</dt>
						<dd class="text-ink num">{formatBytes(status.disk.volumes_bytes)}</dd>
					</dl>
					<div class="mt-3 flex items-center justify-between border-t border-rule pt-3 text-sm">
						<span class="font-medium text-ink">Total</span>
						<span class="font-medium text-ink num">{formatBytes(diskTotal)}</span>
					</div>
				</Panel>
			</div>
		</div>
	{/if}
{/if}

<Modal bind:open={confirmOpen} title="Update presso" dismissable={starting === null}>
	<p>
		presso {update?.current} will be replaced by <span class="num">{update?.latest}</span>. The
		engine downloads and verifies the new binary, waits for running site jobs to finish, then
		restarts itself: the panel is unavailable for a few seconds.
	</p>
	<p class="mt-3">
		After the restart every site is moved to the runtime images of the new release, one container at
		a time. Each site is interrupted for a few seconds while its container is recreated; the edge is
		recreated once as well.
	</p>
	<p class="mt-3 text-xs text-ink-3">
		The previous binary is kept next to the new one for a manual rollback.
	</p>
	{#snippet footer()}
		<Button onclick={() => (confirmOpen = false)} disabled={starting !== null}>Cancel</Button>
		<Button variant="primary" onclick={() => run('update')} busy={starting === 'update'}>
			Update to {update?.latest}
		</Button>
	{/snippet}
</Modal>
