<!-- Overview tab: lifecycle actions, live job panel, delete dialog, info grid. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { formatDateTime, formatNumber, percent } from '$lib/format';
	import type { Job } from '$lib/types/job';
	import type { LoginLinkResponse, Site, SiteAction } from '$lib/types/site';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import JobPanel from '$lib/components/JobPanel.svelte';
	import Modal from '$lib/components/Modal.svelte';

	let { site, activeJob, onJob }: { site: Site; activeJob: Job | null; onJob: (j: Job) => void } =
		$props();

	let busy = $state<string | null>(null);
	let error = $state('');
	let notice = $state('');

	const jobRunning = $derived(
		activeJob !== null && (activeJob.status === 'queued' || activeJob.status === 'running')
	);
	const locked = $derived(
		jobRunning || site.status === 'provisioning' || site.status === 'deleting'
	);

	async function action(name: SiteAction) {
		busy = name;
		error = '';
		notice = '';
		try {
			const res = await api.post<{ job?: Job; ok?: boolean }>(
				`/v1/sites/${site.id}/actions/${name}`
			);
			if (res?.job) onJob(res.job);
			else if (res?.ok)
				notice = name === 'purge-cache' ? 'Page cache purged.' : 'Object cache flushed.';
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = null;
		}
	}

	async function openAdmin() {
		busy = 'login-link';
		error = '';
		try {
			const res = await api.post<LoginLinkResponse>(`/v1/sites/${site.id}/login-link`);
			window.open(res.url, '_blank', 'noopener');
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = null;
		}
	}

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
			await goto(resolve(`/jobs/${res.job.id}`));
		} catch (err) {
			deleteError = errorText(err);
		} finally {
			busy = null;
		}
	}

	const m = $derived(site.metrics_1h);
</script>

<div class="flex flex-col gap-4">
	<div class="flex flex-wrap items-center gap-2">
		<Button
			variant="primary"
			onclick={openAdmin}
			busy={busy === 'login-link'}
			disabled={site.status !== 'running'}
		>
			Open wp-admin
		</Button>
		<span class="mx-1 h-5 w-px bg-gray-200"></span>
		{#if site.status === 'stopped' || site.status === 'error'}
			<Button onclick={() => action('start')} busy={busy === 'start'} disabled={locked}
				>Start</Button
			>
		{/if}
		{#if site.status === 'running'}
			<Button onclick={() => action('stop')} busy={busy === 'stop'} disabled={locked}>Stop</Button>
			<Button onclick={() => action('restart')} busy={busy === 'restart'} disabled={locked}>
				Restart
			</Button>
		{/if}
		<Button onclick={() => action('recreate')} busy={busy === 'recreate'} disabled={locked}>
			Recreate
		</Button>
		{#if site.status === 'error'}
			<Button onclick={() => action('retry')} busy={busy === 'retry'} disabled={locked}
				>Retry</Button
			>
		{/if}
		<span class="mx-1 h-5 w-px bg-gray-200"></span>
		<Button
			onclick={() => action('purge-cache')}
			busy={busy === 'purge-cache'}
			disabled={site.status !== 'running'}
		>
			Purge cache
		</Button>
		<Button
			onclick={() => action('flush-object-cache')}
			busy={busy === 'flush-object-cache'}
			disabled={site.status !== 'running'}
		>
			Flush object cache
		</Button>
		<span class="flex-1"></span>
		<Button
			variant="danger"
			onclick={() => {
				confirmSlug = '';
				keepVolume = false;
				deleteError = '';
				deleteOpen = true;
			}}
			disabled={site.status === 'deleting'}
		>
			Delete
		</Button>
	</div>

	{#if error}
		<Alert>{error}</Alert>
	{/if}
	{#if notice}
		<Alert tone="success">{notice}</Alert>
	{/if}

	{#if activeJob}
		<Card title={jobRunning ? 'Operation in progress' : 'Last operation'}>
			<JobPanel job={activeJob} compact />
		</Card>
	{/if}

	<div class="grid gap-4 lg:grid-cols-3">
		<Card title="Configuration" class="lg:col-span-2">
			<dl class="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
				{#snippet row(label: string, value: string)}
					<div>
						<dt class="text-xs text-gray-500">{label}</dt>
						<dd class="mt-0.5 font-medium break-all text-gray-900">{value}</dd>
					</div>
				{/snippet}
				{@render row('PHP', site.php_version)}
				{@render row('Image', site.image || '–')}
				{@render row('Profile', site.profile)}
				{@render row('File modifications', site.file_mods)}
				{@render row('Page cache', site.page_cache ? 'on' : 'off')}
				{@render row('TLS', site.tls_mode)}
				{@render row('Admin user', site.admin_user)}
				{@render row('Admin email', site.admin_email)}
				{@render row('Created', formatDateTime(site.created_at))}
			</dl>
			{#if site.domains.length > 1}
				<div class="mt-4 border-t border-gray-100 pt-3">
					<p class="text-xs text-gray-500">Domains</p>
					<ul class="mt-1 flex flex-wrap gap-2 text-sm">
						{#each site.domains as d (d.id)}
							<li class="rounded bg-gray-100 px-2 py-0.5 font-mono text-xs">
								{d.host}
								{#if d.kind === 'primary'}<span class="text-gray-500"> (primary)</span>{/if}
							</li>
						{/each}
					</ul>
				</div>
			{/if}
		</Card>

		<Card title="Last hour">
			{#if m}
				<dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
					<div>
						<dt class="text-xs text-gray-500">Requests</dt>
						<dd class="mt-0.5 text-lg font-semibold tabular-nums">{formatNumber(m.requests)}</dd>
					</div>
					<div>
						<dt class="text-xs text-gray-500">Cache hit</dt>
						<dd class="mt-0.5 text-lg font-semibold tabular-nums">
							{percent(m.cache_hit, m.requests)}
						</dd>
					</div>
					<div>
						<dt class="text-xs text-gray-500">Miss / bypass / stale</dt>
						<dd class="mt-0.5 font-medium tabular-nums">
							{formatNumber(m.cache_miss)} / {formatNumber(m.cache_bypass)} / {formatNumber(
								m.cache_stale
							)}
						</dd>
					</div>
					<div>
						<dt class="text-xs text-gray-500">5xx</dt>
						<dd class="mt-0.5 font-medium tabular-nums {m.status_5xx ? 'text-red-700' : ''}">
							{formatNumber(m.status_5xx)}
						</dd>
					</div>
				</dl>
			{:else}
				<p class="text-sm text-gray-500">No traffic recorded yet.</p>
			{/if}
		</Card>
	</div>
</div>

<Modal bind:open={deleteOpen} title="Delete {site.name}">
	<p>
		This stops and removes the site's container and configuration. Type
		<code class="rounded bg-gray-100 px-1 font-mono">{site.slug}</code> to confirm.
	</p>
	<input
		class="mt-3 input font-mono"
		bind:value={confirmSlug}
		placeholder={site.slug}
		autocomplete="off"
	/>
	<label class="mt-3 flex items-center gap-2 text-sm">
		<input type="checkbox" bind:checked={keepVolume} />
		Keep the data volume (files and uploads) for later recovery
	</label>
	{#if deleteError}
		<div class="mt-3"><Alert>{deleteError}</Alert></div>
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
