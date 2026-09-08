<!-- New site, in four steps: Basics → WordPress → Administrator → Review.
     Mounted once in the app shell and opened through state/create-site, so
     the draft outlives an accidental escape; "Start over" blanks it. Enter
     advances a step (except inside the version picker), the footer button
     submits the same form. A successful POST lands on the site page where
     provisioning streams in. -->
<script lang="ts">
	import { tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, ApiError, errorText } from '$lib/api/client';
	import { generatePassword } from '$lib/password';
	import { slugify } from '$lib/slug';
	import {
		capabilities,
		findBranch,
		loadCapabilities,
		phpOptionsFor,
		pickerRows,
		runtimeLabel,
		runtimeOfPhp
	} from '$lib/state/capabilities.svelte';
	import { closeCreateSite, createSite } from '$lib/state/create-site.svelte';
	import { seedJob, sites as liveSites } from '$lib/state/live.svelte';
	import type { Job } from '$lib/types/job';
	import type { CreateSiteRequest, Site } from '$lib/types/site';
	import Alert from '../Alert.svelte';
	import Button from '../Button.svelte';
	import Field from '../Field.svelte';
	import Icon from '../Icon.svelte';
	import Input from '../Input.svelte';
	import Modal from '../Modal.svelte';
	import RuntimeBadge from '../RuntimeBadge.svelte';
	import Segmented from '../Segmented.svelte';
	import Select from '../Select.svelte';
	import Toggle from '../Toggle.svelte';
	import Stepper from './Stepper.svelte';
	import VersionPicker from './VersionPicker.svelte';
	import {
		blankDraft,
		FIELD_IDS,
		STEPS,
		stepForServerError,
		validateStep,
		type Draft,
		type FieldErrors,
		type FieldName
	} from './validate';

	let draft = $state<Draft>(blankDraft());
	let step = $state(0);
	let errors = $state<FieldErrors>({});
	let banner = $state('');
	let submitting = $state(false);
	let capsError = $state('');

	const open = $derived(createSite.open);
	const caps = $derived(capabilities.current);
	const rows = $derived(pickerRows(caps));
	const branch = $derived(findBranch(caps, draft.wp));
	const phpOptions = $derived(phpOptionsFor(caps, branch));
	const phpRuntime = $derived(runtimeOfPhp(caps, draft.php) ?? branch?.runtime ?? 'frankenphp');
	const runtimeChanged = $derived(!!branch && phpRuntime !== branch.runtime);
	const last = STEPS.length - 1;

	const domainPlaceholder = $derived(draft.slug ? `${draft.slug}.localhost` : 'my-site.localhost');
	const effectiveDomain = $derived(draft.domain.trim().toLowerCase() || domainPlaceholder);
	const isLocal = $derived(effectiveDomain.endsWith('.localhost'));
	const previewUrl = $derived(`${isLocal ? 'http' : 'https'}://${effectiveDomain}`);

	const dirty = $derived.by(() => {
		const b = blankDraft();
		return (
			step > 0 ||
			draft.name !== b.name ||
			draft.slug !== b.slug ||
			draft.domain !== b.domain ||
			draft.phpOverridden ||
			draft.profile !== b.profile ||
			draft.pageCache !== b.pageCache ||
			draft.adminUser !== b.adminUser ||
			draft.adminEmail !== b.adminEmail ||
			draft.adminPassword !== b.adminPassword ||
			(!!caps && draft.wp !== caps.default_wp)
		);
	});

	function loadCaps() {
		capsError = '';
		loadCapabilities().catch((err) => (capsError = errorText(err)));
	}
	$effect(() => {
		if (open) loadCaps();
	});
	$effect(() => {
		if (open) focusStep(step);
	});

	// The slug follows the name until it is typed by hand.
	$effect(() => {
		if (!draft.slugEdited) draft.slug = slugify(draft.name);
	});

	// Seed the WordPress choice from the server default, and fall back to it
	// when the draft's branch is no longer offered.
	$effect(() => {
		if (!caps) return;
		if (!rows.some((r) => r.branch === draft.wp && !r.disabled)) draft.wp = caps.default_wp;
	});

	// PHP follows the branch's recommendation unless overridden with a version
	// the branch still supports.
	$effect(() => {
		const b = branch;
		if (!b) return;
		if (draft.phpOverridden && b.php_versions.includes(draft.php)) return;
		if (draft.php !== b.default_php) draft.php = b.default_php;
		if (draft.phpOverridden) draft.phpOverridden = false;
	});

	async function focusStep(i: number) {
		await tick();
		const id =
			i === 0
				? FIELD_IDS.name
				: i === 1
					? FIELD_IDS.wp
					: i === 2
						? FIELD_IDS.adminUser
						: 'cs-review';
		document.getElementById(id)?.focus();
	}
	async function focusField(f: FieldName) {
		await tick();
		document.getElementById(FIELD_IDS[f])?.focus();
	}

	function goTo(i: number) {
		if (submitting || i >= step) return;
		step = i;
		errors = {};
		banner = '';
	}
	function back() {
		goTo(step - 1);
	}
	function startOver() {
		draft = blankDraft();
		step = 0;
		errors = {};
		banner = '';
		focusStep(0);
	}

	async function onSubmit(e: SubmitEvent) {
		e.preventDefault();
		if (submitting) return;
		if (step === 1 && !caps) return;
		const errs = validateStep(step, draft, caps);
		errors = errs;
		const first = (Object.keys(errs) as FieldName[])[0];
		if (first) {
			focusField(first);
			return;
		}
		banner = '';
		if (step < last) {
			step++;
			return;
		}
		await submit();
	}

	async function submit() {
		const b = branch;
		if (!b) return;
		submitting = true;
		banner = '';
		const domain = draft.domain.trim().toLowerCase();
		const body: CreateSiteRequest = {
			name: draft.name.trim(),
			slug: draft.slug.trim(),
			wp_version: b.version,
			php_version: draft.php,
			page_cache: draft.pageCache,
			profile: draft.profile,
			admin_user: draft.adminUser.trim(),
			admin_email: draft.adminEmail.trim(),
			admin_password: draft.adminPassword
		};
		if (domain) body.primary_domain = domain;
		if (isLocal) body.tls_mode = 'off';
		try {
			const res = await api.post<{ site: Site; job: Job }>('/v1/sites', body);
			liveSites.set(res.site.id, res.site);
			if (res.job) seedJob(res.job);
			draft = blankDraft();
			step = 0;
			errors = {};
			closeCreateSite();
			await goto(resolve(`/sites/${res.site.id}`));
		} catch (err) {
			const target = stepForServerError(err instanceof ApiError ? err.code : '');
			step = target.step;
			if (target.field) {
				errors = { [target.field]: errorText(err) };
				focusField(target.field);
			} else {
				banner = errorText(err);
			}
		} finally {
			submitting = false;
		}
	}

	function resetPhp() {
		if (!branch) return;
		draft.php = branch.default_php;
		draft.phpOverridden = false;
	}

	/** aria-invalid and the error link for a field. */
	function aria(f: FieldName) {
		return errors[f]
			? { 'aria-invalid': true as const, 'aria-describedby': `${FIELD_IDS[f]}-error` }
			: {};
	}

	const nextSteps = $derived([
		'Pull the runtime image if this host has not seen it yet',
		'Create the database and a Redis cache user for the site',
		'Start the container and wait until it reports healthy',
		branch
			? `Install WordPress ${branch.version} with the administrator account`
			: 'Install WordPress with the administrator account',
		'Publish the route at the edge and warm the front page'
	]);
</script>

{#snippet summary(title: string, to: number, entries: [string, string][])}
	<section class="rounded-plate border border-rule">
		<header class="flex items-center justify-between gap-4 border-b border-rule px-4 py-2.5">
			<h3 class="text-sm font-medium text-ink">{title}</h3>
			<button
				type="button"
				class="text-xs font-medium text-brand hover:text-brand-hover"
				onclick={() => goTo(to)}
				aria-label="Edit {title.toLowerCase()}"
			>
				Edit
			</button>
		</header>
		<dl class="grid gap-x-6 gap-y-2 px-4 py-3 sm:grid-cols-[9rem_minmax(0,1fr)]">
			{#each entries as [k, v] (k)}
				<dt class="text-xs text-ink-2 sm:pt-0.5">{k}</dt>
				<dd class="min-w-0 truncate text-sm text-ink" title={v}>{v}</dd>
			{/each}
		</dl>
	</section>
{/snippet}

<Modal bind:open={createSite.open} title="New site" size="lg" dismissable={!submitting}>
	{#snippet header()}
		<Stepper steps={STEPS} current={step} onselect={goTo} />
	{/snippet}

	<form id="cs-form" onsubmit={onSubmit} novalidate class="flex flex-col gap-5">
		{#if step === 0}
			<div class="grid gap-5 sm:grid-cols-2">
				<Field label="Name" for={FIELD_IDS.name} error={errors.name}>
					<Input
						id={FIELD_IDS.name}
						bind:value={draft.name}
						placeholder="Acme Bakery"
						autocomplete="off"
						invalid={!!errors.name}
						{...aria('name')}
					/>
				</Field>
				<Field
					label="Slug"
					for={FIELD_IDS.slug}
					hint="Names the container and volume. Lowercase, digits, hyphens."
					error={errors.slug}
				>
					<Input
						id={FIELD_IDS.slug}
						mono
						bind:value={draft.slug}
						oninput={() => (draft.slugEdited = true)}
						autocomplete="off"
						spellcheck="false"
						invalid={!!errors.slug}
						{...aria('slug')}
					/>
				</Field>
				<Field
					label="Primary domain"
					for={FIELD_IDS.domain}
					class="sm:col-span-2"
					hint={isLocal
						? 'Optional. A .localhost domain resolves to this machine; no certificate is needed.'
						: 'Point the DNS at this host. A certificate is obtained automatically.'}
					error={errors.domain}
				>
					<Input
						id={FIELD_IDS.domain}
						bind:value={draft.domain}
						placeholder={domainPlaceholder}
						autocomplete="off"
						spellcheck="false"
						invalid={!!errors.domain}
						{...aria('domain')}
					/>
				</Field>
			</div>
			<div class="rounded-control bg-sunken px-4 py-3">
				<p class="text-xs text-ink-2">Address</p>
				<p class="mt-0.5 font-mono text-sm break-all text-ink">{previewUrl}</p>
				<p class="mt-1 text-xs text-ink-3">
					{isLocal ? 'Opens from this machine only.' : 'Public once DNS points here.'}
				</p>
			</div>
		{:else if step === 1}
			{#if capsError}
				<Alert>
					{capsError}
					<button type="button" class="ml-1 underline underline-offset-2" onclick={loadCaps}>
						Try again
					</button>
				</Alert>
			{:else if !caps}
				<p class="py-6 text-center text-sm text-ink-3" aria-busy="true">
					Loading the available versions.
				</p>
			{:else}
				{#if banner}
					<Alert>{banner}</Alert>
				{/if}
				<Field label="WordPress version" for={FIELD_IDS.wp} error={errors.wp}>
					<VersionPicker
						id={FIELD_IDS.wp}
						{rows}
						bind:value={draft.wp}
						defaultBranch={caps.default_wp}
						invalid={!!errors.wp}
					/>
				</Field>
				{#if branch}
					<p class="flex flex-wrap items-center gap-2 text-sm text-ink-2">
						<span>
							WordPress {branch.branch} runs on PHP {draft.php} ({runtimeLabel(phpRuntime)}).
						</span>
						<RuntimeBadge runtime={phpRuntime} />
					</p>
				{/if}

				<div class="border-t border-rule pt-4">
					<button
						type="button"
						class="flex items-center gap-1.5 text-sm font-medium text-ink-2 hover:text-ink"
						aria-expanded={draft.advancedOpen}
						aria-controls="cs-advanced"
						onclick={() => (draft.advancedOpen = !draft.advancedOpen)}
					>
						<Icon
							name="chevron-right"
							size={16}
							class="transition-transform {draft.advancedOpen ? 'rotate-90' : ''}"
						/>
						Advanced
					</button>
					{#if draft.advancedOpen}
						<div id="cs-advanced" class="mt-4 flex flex-col gap-4">
							<Field
								label="PHP version"
								for={FIELD_IDS.php}
								hint={draft.phpOverridden
									? undefined
									: 'Recommended: the newest PHP this WordPress version supports.'}
								error={errors.php}
							>
								<div class="flex flex-wrap items-center gap-3">
									<Select
										id={FIELD_IDS.php}
										bind:value={draft.php}
										options={phpOptions}
										disabled={!branch}
										class="min-w-56"
										onchange={(v) => (draft.phpOverridden = v !== branch?.default_php)}
									/>
									{#if draft.phpOverridden}
										<Button variant="ghost" onclick={resetPhp}>Reset to recommended</Button>
									{/if}
								</div>
							</Field>
							{#if runtimeChanged && branch}
								<Alert tone="warn">
									PHP {draft.php} moves this site to {runtimeLabel(phpRuntime)} instead of
									{runtimeLabel(branch.runtime)}.
									{#if phpRuntime === 'legacy'}
										The legacy runtime exists for WordPress releases that cannot run on PHP 8.2 or
										newer.
									{/if}
								</Alert>
							{/if}
						</div>
					{/if}
				</div>

				<div class="grid gap-5 border-t border-rule pt-4 sm:grid-cols-2">
					<Field
						label="Profile"
						for="cs-profile"
						hint={draft.profile === 'dev'
							? 'Debug logging on, caches short. For building, not for visitors.'
							: 'Tuned for visitors: opcache warm, errors logged not shown.'}
					>
						<div>
							<Segmented
								bind:value={draft.profile}
								label="Profile"
								options={[
									{ value: 'prod', label: 'Production' },
									{ value: 'dev', label: 'Development' }
								]}
							/>
						</div>
					</Field>
					<div class="flex items-start pt-1 sm:pt-8">
						<Toggle
							bind:checked={draft.pageCache}
							label="Page cache at the edge"
							hint="Anonymous page views are served from the edge and purged when content changes."
						/>
					</div>
				</div>
			{/if}
		{:else if step === 2}
			<div class="grid gap-5 sm:grid-cols-2">
				<Field label="Username" for={FIELD_IDS.adminUser} error={errors.adminUser}>
					<Input
						id={FIELD_IDS.adminUser}
						bind:value={draft.adminUser}
						autocomplete="off"
						spellcheck="false"
						invalid={!!errors.adminUser}
						{...aria('adminUser')}
					/>
				</Field>
				<Field label="Email" for={FIELD_IDS.adminEmail} error={errors.adminEmail}>
					<Input
						id={FIELD_IDS.adminEmail}
						type="email"
						bind:value={draft.adminEmail}
						autocomplete="off"
						spellcheck="false"
						invalid={!!errors.adminEmail}
						{...aria('adminEmail')}
					/>
				</Field>
				<Field
					label="Password"
					for={FIELD_IDS.adminPassword}
					class="sm:col-span-2"
					hint="At least 12 characters. Stored only inside WordPress."
					error={errors.adminPassword}
				>
					<div class="flex gap-2">
						<Input
							id={FIELD_IDS.adminPassword}
							type="password"
							bind:shown={draft.showPassword}
							mono
							class="flex-1"
							bind:value={draft.adminPassword}
							autocomplete="new-password"
							spellcheck="false"
							invalid={!!errors.adminPassword}
							{...aria('adminPassword')}
						/>
						<Button
							onclick={() => {
								draft.adminPassword = generatePassword(20);
								draft.showPassword = true;
							}}
						>
							Generate
						</Button>
					</div>
				</Field>
			</div>
			<p class="text-xs text-ink-3">
				The first WordPress account. Sign in later with one click from the site page.
			</p>
		{:else}
			<div id="cs-review" tabindex="-1" class="flex flex-col gap-4 outline-none">
				{#if banner}
					<Alert>{banner}</Alert>
				{/if}
				{@render summary('Site', 0, [
					['Name', draft.name.trim()],
					['Slug', draft.slug],
					['Address', previewUrl]
				])}
				{@render summary('WordPress', 1, [
					['Version', branch ? `WordPress ${branch.version}` : '–'],
					['PHP', `${draft.php} · ${runtimeLabel(phpRuntime)}`],
					['Profile', draft.profile === 'dev' ? 'Development' : 'Production'],
					['Page cache', draft.pageCache ? 'On' : 'Off']
				])}
				{@render summary('Administrator', 2, [
					['Username', draft.adminUser.trim()],
					['Email', draft.adminEmail.trim()],
					[
						'Password',
						draft.adminPassword ? `•••••••• (${draft.adminPassword.length} characters)` : '–'
					]
				])}
				{#if phpRuntime === 'legacy'}
					<Alert tone="info">
						<span class="flex flex-wrap items-center gap-2">
							<RuntimeBadge runtime="legacy" />
							This site runs on nginx + PHP-FPM and is marked Legacy wherever it appears.
						</span>
					</Alert>
				{/if}
				<div>
					<p class="text-sm font-medium text-ink">What happens next</p>
					<ol class="mt-2 flex flex-col gap-2">
						{#each nextSteps as s, i (i)}
							<li class="flex gap-3 text-sm leading-5 text-ink-2">
								<span class="mt-px w-4 shrink-0 display text-xs text-ink-3 num">{i + 1}</span>
								<span>{s}</span>
							</li>
						{/each}
					</ol>
					<p class="mt-3 flex items-start gap-2 border-t border-rule pt-3 text-xs text-ink-3">
						<Icon name="camera" size={14} class="mt-0.5" />
						A preview of the front page is captured at the end and shown on the wall.
					</p>
				</div>
			</div>
		{/if}
	</form>

	{#snippet footer()}
		{#if dirty}
			<Button variant="ghost" class="mr-auto" onclick={startOver} disabled={submitting}>
				Start over
			</Button>
		{/if}
		{#if step > 0}
			<Button onclick={back} disabled={submitting}>Back</Button>
		{/if}
		<Button
			type="submit"
			form="cs-form"
			variant="primary"
			busy={submitting}
			disabled={step === 1 && !caps}
		>
			{step === last ? 'Create site' : 'Next'}
		</Button>
	{/snippet}
</Modal>
