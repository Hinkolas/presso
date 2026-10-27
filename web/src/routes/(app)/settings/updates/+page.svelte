<!-- Settings → Updates: the installed version, the newest release on the
     chosen channel (GitHub Releases), the update itself with live progress
     and the update settings (channel, automatic updates).

     Progress spans two engine jobs and a restart: engine.update prepares,
     downloads, pre-pulls images and swaps the binary, the engine re-execs,
     and the engine.rollout job the new engine starts on boot moves every
     site to the new runtime images. The page follows both over the event
     stream; live.svelte.ts reloads the app once the new engine answers, and
     the page picks the rollout up from there. Mutations are admin-only. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { jobEvents, jobs, live, loadJob } from '$lib/state/live.svelte';
	import { isAdmin } from '$lib/state/session.svelte';
	import { jobProgress } from '$lib/job-progress';
	import { formatDate, formatDateTime, formatRelative } from '$lib/format';
	import type { SystemStatus, UpdateChannel, UpdateStatus } from '$lib/types/system';
	import type { Job } from '$lib/types/job';
	import Alert from '$lib/components/Alert.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import JobTimeline from '$lib/components/JobTimeline.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import ProgressBar from '$lib/components/ProgressBar.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import Toggle from '$lib/components/Toggle.svelte';

	let loadError = $state('');
	$effect(() => {
		if (live.update) return;
		api
			.get<SystemStatus>('/v1/system/status')
			.then((r) => {
				if (r.update && !live.update) live.update = r.update;
				if (!r.update) loadError = 'This engine runs without the updater.';
			})
			.catch((err) => (loadError = errorText(err)));
	});

	const update = $derived<UpdateStatus | null>(live.update);
	const admin = $derived(isAdmin());

	// --- The engine job to follow ------------------------------------------
	const isEngine = (j: Job) => j.kind === 'engine.update' || j.kind === 'engine.rollout';
	const engineJob = $derived.by<Job | null>(() => {
		if (update?.job_id) {
			const j = jobs.get(update.job_id);
			if (j) return j;
		}
		let found: Job | null = null;
		for (const j of jobs.values()) {
			if (!isEngine(j) || (j.status !== 'queued' && j.status !== 'running')) continue;
			if (!found || j.created_at > found.created_at) found = j;
		}
		return found;
	});
	// The most recent update attempt, to report a failure once it is over.
	const lastUpdate = $derived.by<Job | null>(() => {
		let found: Job | null = null;
		for (const j of jobs.values()) {
			if (j.kind === 'engine.update' && (!found || j.created_at > found.created_at)) found = j;
		}
		return found;
	});
	const failedUpdate = $derived(
		!engineJob && lastUpdate?.status === 'failed' && update?.available ? lastUpdate : null
	);

	// Fetch the event log of a job this page did not see start.
	$effect(() => {
		const id = engineJob?.id;
		if (id && !jobEvents.has(id)) loadJob(id).catch(() => {});
	});

	// A rollout the engine started on boot is the last phase of an update; one
	// started by hand stands alone.
	const followsUpdate = $derived(
		!!live.restarting ||
			engineJob?.kind === 'engine.update' ||
			(engineJob?.kind === 'engine.rollout' && engineJob.created_by === 'system')
	);
	const target = $derived.by(() => {
		if (live.restarting && live.restarting !== 'new') return live.restarting;
		const v = engineJob?.args?.version;
		if (typeof v === 'string' && v) return v;
		return engineJob?.kind === 'engine.rollout' ? (update?.current ?? '') : (update?.latest ?? '');
	});

	type Phase = { id: string; label: string; steps: string[] };
	const PHASES: Phase[] = [
		{ id: 'prepare', label: 'Prepare', steps: ['check', 'drain'] },
		{ id: 'download', label: 'Download', steps: ['download'] },
		{ id: 'images', label: 'Images', steps: ['images'] },
		{ id: 'restart', label: 'Restart', steps: ['install', 'restart'] },
		{ id: 'sites', label: 'Sites', steps: ['resolve', 'recreate'] }
	];
	const progress = $derived(
		engineJob ? jobProgress(engineJob, jobEvents.get(engineJob.id) ?? []) : null
	);
	/** Where the update stands: the phase under way and 0..1 overall. */
	const position = $derived.by<{ phase: number; fraction: number | null }>(() => {
		if (live.restarting) return { phase: 3, fraction: 0.8 };
		if (!engineJob || !progress) return { phase: -1, fraction: null };
		if (engineJob.kind === 'engine.rollout') {
			return { phase: 4, fraction: 0.8 + progress.fraction * 0.2 };
		}
		const step = progress.steps[progress.position]?.name ?? '';
		const phase = Math.max(
			0,
			PHASES.findIndex((p) => p.steps.includes(step))
		);
		return { phase, fraction: engineJob.status === 'queued' ? null : progress.fraction * 0.8 };
	});
	/** The newest line the job logged: an image pull, a site being recreated. */
	const latestLine = $derived.by(() => {
		if (!engineJob) return '';
		const evs = jobEvents.get(engineJob.id) ?? [];
		for (let i = evs.length - 1; i >= 0; i--) {
			if (evs[i].level !== 'step') return evs[i].message;
		}
		return '';
	});
	const busy = $derived(!!engineJob || !!live.restarting);
	let showDetails = $state(false);

	// --- Actions ---------------------------------------------------------
	let actionError = $state('');
	let checking = $state(false);
	async function checkNow() {
		checking = true;
		actionError = '';
		try {
			const r = await api.post<{ update: UpdateStatus }>('/v1/system/actions/check-update', {});
			live.update = r.update;
		} catch (err) {
			actionError = errorText(err);
		} finally {
			checking = false;
		}
	}

	let confirmOpen = $state(false);
	let starting = $state<'update' | 'rollout' | null>(null);
	async function run(action: 'update' | 'rollout') {
		starting = action;
		actionError = '';
		try {
			const r = await api.post<{ job: Job }>(`/v1/system/actions/${action}`, {});
			jobs.set(r.job.id, r.job);
			if (live.update) live.update = { ...live.update, job_id: r.job.id, job_kind: r.job.kind };
			confirmOpen = false;
			showDetails = false;
		} catch (err) {
			actionError = errorText(err);
		} finally {
			starting = null;
		}
	}

	let saving = $state(false);
	let settingsError = $state('');
	async function save(patch: { channel?: UpdateChannel; auto_update?: boolean }) {
		saving = true;
		settingsError = '';
		try {
			const r = await api.put<{ update: UpdateStatus }>('/v1/system/update/settings', patch);
			live.update = r.update;
		} catch (err) {
			settingsError = errorText(err);
		} finally {
			saving = false;
		}
	}
	let channel = $state<UpdateChannel>('stable');
	let autoUpdate = $state(false);
	$effect(() => {
		if (!update) return;
		channel = update.channel;
		autoUpdate = update.auto_update;
	});

	// --- Wording ---------------------------------------------------------
	const feedProblem: Record<string, string> = {
		offline: 'presso could not reach GitHub. Check the server’s internet connection and DNS.',
		not_found:
			'The release repository was not found on GitHub. It may be private or renamed (UPDATE_REPO).',
		rate_limited:
			'GitHub’s limit for anonymous requests from this server is used up. The next check tries again.',
		unavailable: 'GitHub answered with an error. The next check tries again.',
		invalid: 'GitHub answered with something presso could not read.',
		no_release: 'Nothing has been published on this channel yet.'
	};
	function shortHost(url: string): string {
		try {
			return new URL(url).host;
		} catch {
			return url;
		}
	}
	const channels: { value: UpdateChannel; label: string }[] = [
		{ value: 'stable', label: 'Stable' },
		{ value: 'beta', label: 'Beta' }
	];
</script>

<svelte:head>
	<title>Updates · presso</title>
</svelte:head>

<PageHeader
	title="Updates"
	subtitle={update
		? `presso ${update.current} · ${update.channel === 'beta' ? 'Beta' : 'Stable'} channel`
		: 'Loading the update status.'}
/>

{#if loadError && !update}
	<Alert>{loadError}</Alert>
{:else if !update}
	<div class="flex flex-col gap-5" aria-busy="true">
		<Skeleton plate class="h-40" />
		<Skeleton plate class="h-48" />
	</div>
{:else}
	<div class="flex flex-col gap-5">
		<!-- Status -->
		<Panel>
			<div class="flex flex-col gap-5">
				<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
					<div class="flex min-w-0 items-start gap-4">
						<span
							class="grid size-11 shrink-0 place-items-center rounded-control {busy
								? 'bg-busy-soft text-busy-ink'
								: update.available
									? 'bg-brand-soft text-brand'
									: update.latest && !update.error
										? 'bg-ok-soft text-ok'
										: 'bg-sunken text-ink-2'}"
						>
							<Icon
								name={busy
									? 'refresh'
									: update.available
										? 'plus'
										: update.latest && !update.error
											? 'check'
											: 'clock'}
								size={22}
								class={busy ? 'animate-spin [animation-duration:2.5s]' : ''}
							/>
						</span>
						<div class="min-w-0">
							{#if live.restarting}
								<h2 class="display text-xl text-ink">Restarting into {target}</h2>
								<p class="mt-1 text-sm text-ink-2">
									The engine is restarting. This page reloads on its own once it is back, then
									follows the sites onto the new release.
								</p>
							{:else if engineJob}
								<h2 class="display text-xl text-ink">
									{#if engineJob.kind === 'engine.update'}
										Updating to {target}
									{:else if followsUpdate}
										Moving sites to {target}
									{:else}
										Rolling out runtime images
									{/if}
								</h2>
								<p class="mt-1 text-sm text-ink-2">
									{progress?.current?.message ||
										(engineJob.status === 'queued' ? 'Waiting to start' : 'Working')}
								</p>
							{:else if update.available}
								<h2 class="flex flex-wrap items-center gap-2 display text-xl text-ink">
									presso <span class="num">{update.latest}</span> is available
									{#if update.prerelease}<Badge tone="busy">Beta</Badge>{/if}
								</h2>
								<p class="mt-1 text-sm text-ink-2">
									You have {update.current}.
									{#if update.published_at}Released {formatDate(update.published_at)}.{/if}
									{#if update.url}
										<!-- eslint-disable svelte/no-navigation-without-resolve -- external release URL -->
										<a
											href={update.url}
											target="_blank"
											rel="noreferrer"
											class="inline-flex items-center gap-1 text-ink underline decoration-rule-strong hover:decoration-ink"
											>Release notes on {shortHost(update.url)}<Icon name="external" size={14} /></a
										>
										<!-- eslint-enable svelte/no-navigation-without-resolve -->
									{/if}
								</p>
							{:else if update.ahead}
								<h2 class="display text-xl text-ink">Ahead of the stable channel</h2>
								<p class="mt-1 text-sm text-ink-2">
									You run {update.current}, newer than the latest stable release ({update.latest}).
									presso never downgrades: the next stable release newer than yours will be offered.
								</p>
							{:else if update.latest}
								<h2 class="display text-xl text-ink">presso is up to date</h2>
								<p class="mt-1 text-sm text-ink-2">
									{update.current} is the newest {update.channel === 'beta'
										? 'beta or stable'
										: 'stable'}
									release.
								</p>
							{:else if update.error}
								<h2 class="display text-xl text-ink">No release information</h2>
								<p class="mt-1 text-sm text-ink-2">
									The last check did not get an answer about the newest release.
								</p>
							{:else}
								<h2 class="display text-xl text-ink">Not checked yet</h2>
								<p class="mt-1 text-sm text-ink-2">
									presso checks GitHub Releases shortly after it starts.
								</p>
							{/if}
						</div>
					</div>
					<div class="flex shrink-0 flex-wrap gap-2">
						{#if !busy}
							<Button size="sm" onclick={checkNow} busy={checking} disabled={!admin}
								>{update.checked_at ? 'Check again' : 'Check for updates'}</Button
							>
						{/if}
						{#if update.available && !busy}
							<Button
								size="sm"
								variant="primary"
								onclick={() => (confirmOpen = true)}
								disabled={!admin || !update.supported}
								title={!update.supported
									? update.unsupported_reason
									: !admin
										? 'Only administrators can update presso'
										: undefined}
							>
								Update now
							</Button>
						{/if}
					</div>
				</div>

				{#if busy}
					<div class="flex flex-col gap-3">
						{#if followsUpdate}
							<ol class="grid grid-cols-5 gap-2" aria-label="Update phases">
								{#each PHASES as p, i (p.id)}
									<li class="flex flex-col gap-1.5">
										<span
											class="h-1 rounded-full {i < position.phase
												? 'bg-ok'
												: i === position.phase
													? 'breathe bg-busy'
													: 'bg-rule'}"
										></span>
										<span
											class="text-xs {i === position.phase
												? 'font-medium text-ink'
												: i < position.phase
													? 'text-ink-2'
													: 'text-ink-3'}">{p.label}</span
										>
									</li>
								{/each}
							</ol>
						{:else}
							<ProgressBar value={progress?.fraction ?? null} label="Rollout progress" />
						{/if}
						{#if followsUpdate}
							<ProgressBar value={position.fraction} label="Update progress" size="sm" />
						{/if}
						{#if latestLine && !live.restarting}
							<p class="truncate font-mono text-xs text-ink-3" title={latestLine}>{latestLine}</p>
						{/if}
						{#if engineJob}
							<div class="flex flex-wrap gap-x-4 gap-y-1 text-xs">
								<button
									type="button"
									class="text-ink-2 underline decoration-rule-strong hover:text-ink"
									onclick={() => (showDetails = !showDetails)}
									aria-expanded={showDetails}
									>{showDetails ? 'Hide details' : 'Show details'}</button
								>
								<a
									href={resolve(`/jobs/${engineJob.id}`)}
									class="text-ink-2 underline decoration-rule-strong hover:text-ink">Open the job</a
								>
							</div>
							{#if showDetails}
								<JobTimeline job={engineJob} class="border-t border-rule pt-4" />
							{/if}
						{/if}
					</div>
				{/if}

				{#if failedUpdate}
					<Alert>
						The update to {failedUpdate.args?.version ?? update.latest} failed{failedUpdate.error
							? `: ${failedUpdate.error}`
							: '.'}
						Nothing was replaced unless the job got past “Install binary”.
						<a href={resolve(`/jobs/${failedUpdate.id}`)} class="font-medium underline"
							>See what happened</a
						>
					</Alert>
				{/if}
				{#if update.error && !busy}
					<Alert tone={update.latest ? 'warn' : 'error'}>
						{feedProblem[update.error_kind ?? ''] ?? 'The update check failed.'}
						{#if update.latest}Showing the last known release.{/if}
						<span class="mt-1 block font-mono text-xs opacity-80">{update.error}</span>
					</Alert>
				{/if}
				{#if !update.supported && update.unsupported_reason}
					<Alert tone="info">
						Updates cannot be installed from the panel here: {update.unsupported_reason}. Use
						<span class="font-mono text-xs">install.sh --version</span> to change versions.
					</Alert>
				{/if}
				{#if actionError}
					<Alert>{actionError}</Alert>
				{/if}

				<dl
					class="grid grid-cols-[8rem_minmax(0,1fr)] gap-y-2 border-t border-rule pt-4 text-sm sm:grid-cols-[8rem_minmax(0,1fr)_8rem_minmax(0,1fr)]"
				>
					<dt class="text-ink-2">Installed</dt>
					<dd class="text-ink num">{update.current}</dd>
					<dt class="text-ink-2">Latest on channel</dt>
					<dd class="text-ink">
						{#if update.latest}<span class="num">{update.latest}</span>{:else}<span
								class="text-ink-3">unknown</span
							>{/if}
					</dd>
					<dt class="text-ink-2">Last check</dt>
					<dd class="text-ink" title={formatDateTime(update.checked_at)}>
						{update.checked_at ? formatRelative(update.checked_at) : 'Never'}
					</dd>
					<dt class="text-ink-2">Background check</dt>
					<dd class="text-ink">{update.checks ? 'Every few hours' : 'Off (UPDATE_CHECK)'}</dd>
				</dl>
			</div>
		</Panel>

		<!-- Settings -->
		<Panel title="Update settings">
			<div class="flex flex-col gap-6">
				<div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-8">
					<div class="max-w-prose">
						<h3 class="text-sm font-medium text-ink">Release channel</h3>
						<p class="mt-1 text-xs leading-5 text-ink-3">
							Stable offers finished releases only. Beta also offers release candidates and other
							prereleases ahead of a release: earlier fixes, more risk. Switching back to stable
							never downgrades; you stay on your version until a newer stable release exists.
						</p>
					</div>
					<Segmented
						options={channels}
						bind:value={channel}
						size="sm"
						label="Release channel"
						disabled={!admin || saving}
						onchange={(c) => {
							if (c !== update.channel) save({ channel: c });
						}}
					/>
				</div>
				<div class="border-t border-rule pt-5">
					<Toggle
						bind:checked={autoUpdate}
						disabled={!admin || saving}
						label="Install updates automatically"
						hint={update.checks
							? 'When the background check finds a newer release on the channel, presso installs it on its own, the same way as Update now. A failed automatic update is not retried for the same release.'
							: 'Background checks are off (UPDATE_CHECK=false), so automatic updates do not run.'}
						onchange={(v) => save({ auto_update: v })}
					/>
				</div>
				{#if settingsError}
					<Alert>{settingsError}</Alert>
				{/if}
				{#if !admin}
					<p class="text-xs text-ink-3">Only administrators can change update settings.</p>
				{/if}
			</div>
		</Panel>

		<!-- Maintenance -->
		<Panel title="Runtime images">
			<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
				<p class="max-w-prose text-sm text-ink-2">
					Every update moves the sites to the new release’s runtime images on its own. Run it again
					by hand if a site was skipped, for example because it was stopped or failed during the
					update.
				</p>
				<Button
					size="sm"
					onclick={() => run('rollout')}
					busy={starting === 'rollout'}
					disabled={!admin || busy}
				>
					Roll out runtime images
				</Button>
			</div>
		</Panel>
	</div>
{/if}

<Modal bind:open={confirmOpen} title="Update presso" dismissable={starting === null}>
	<p>
		presso {update?.current} will be replaced by
		<span class="num">{update?.latest}</span>{update?.prerelease
			? ', a prerelease from the beta channel'
			: ''}. The engine waits for running site jobs to finish, downloads and verifies the new
		binary, pre-pulls the new images, then restarts itself: the panel is unavailable for a few
		seconds.
	</p>
	<p class="mt-3">
		After the restart every site is moved to the runtime images of the new release, one container at
		a time. Each site is interrupted for a few seconds while its container is recreated; the shared
		services are recreated once as well. You can follow every step on this page.
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
