<!-- New site. The form on the left, what will happen on the right. A
     successful POST lands on the site page where provisioning streams in. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, errorText } from '$lib/api/client';
	import { slugify } from '$lib/slug';
	import { generatePassword } from '$lib/password';
	import type { CreateSiteRequest, Site, SiteProfile } from '$lib/types/site';
	import type { Job } from '$lib/types/job';
	import type { RuntimeInfo } from '$lib/types/system';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import Select from '$lib/components/Select.svelte';
	import Toggle from '$lib/components/Toggle.svelte';

	let runtime = $state<RuntimeInfo | null>(null);
	let name = $state('');
	let slug = $state('');
	let slugEdited = $state(false);
	let domain = $state('');
	let phpVersion = $state('');
	let adminUser = $state('admin');
	let adminEmail = $state('');
	let adminPassword = $state('');
	let showPassword = $state(false);
	let pageCache = $state(true);
	let profile = $state<SiteProfile>('prod');
	let submitting = $state(false);
	let error = $state('');

	$effect(() => {
		api
			.get<RuntimeInfo>('/v1/system/runtime')
			.then((r) => {
				runtime = r;
				if (!phpVersion) phpVersion = r.default_php;
			})
			.catch((err) => (error = errorText(err)));
	});

	$effect(() => {
		if (!slugEdited) slug = slugify(name);
	});

	const domainPlaceholder = $derived(slug ? `${slug}.localhost` : 'my-site.localhost');
	const effectiveDomain = $derived(domain.trim() || domainPlaceholder);
	const isLocal = $derived(effectiveDomain.endsWith('.localhost'));
	const previewUrl = $derived(`${isLocal ? 'http' : 'https'}://${effectiveDomain}`);

	const steps = [
		'Pull the runtime image if this host has not seen it yet',
		'Create the database and a Redis cache user for the site',
		'Start the container and wait until it reports healthy',
		'Install WordPress with the admin account below',
		'Publish the route at the edge and warm the front page'
	];

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		if (!slug) {
			error = 'A slug is required.';
			return;
		}
		submitting = true;
		const body: CreateSiteRequest = {
			name: name.trim() || slug,
			slug,
			php_version: phpVersion,
			page_cache: pageCache,
			profile,
			admin_user: adminUser.trim(),
			admin_email: adminEmail.trim(),
			admin_password: adminPassword
		};
		if (domain.trim()) body.primary_domain = domain.trim();
		if (isLocal) body.tls_mode = 'off';
		try {
			const res = await api.post<{ site: Site; job: Job }>('/v1/sites', body);
			await goto(resolve(`/sites/${res.site.id}`));
		} catch (err) {
			error = errorText(err);
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>New site · presso</title>
</svelte:head>

<PageHeader
	title="New site"
	subtitle="A fresh WordPress install behind the edge cache."
	back={{ href: resolve('/'), label: 'Sites' }}
/>

<form onsubmit={submit} class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
	<div class="flex flex-col gap-5">
		<Panel title="Site">
			<div class="grid gap-5 sm:grid-cols-2">
				<Field label="Name" for="name">
					<input id="name" class="input" bind:value={name} required placeholder="Acme Bakery" />
				</Field>
				<Field
					label="Slug"
					for="slug"
					hint="Names the container and volume. Lowercase, digits, hyphens."
				>
					<input
						id="slug"
						class="input font-mono"
						bind:value={slug}
						oninput={() => (slugEdited = true)}
						pattern="[a-z0-9]+(-[a-z0-9]+)*"
						required
					/>
				</Field>
				<Field
					label="Primary domain"
					for="domain"
					hint={isLocal
						? 'A .localhost domain resolves to this machine; no certificate is needed.'
						: 'Point the DNS at this host. A certificate is obtained automatically.'}
					class="sm:col-span-2"
				>
					<input id="domain" class="input" bind:value={domain} placeholder={domainPlaceholder} />
				</Field>
				<Field label="PHP version" for="php">
					<Select
						id="php"
						block
						bind:value={phpVersion}
						disabled={!runtime}
						options={(runtime?.php_versions ?? []).map((v) => ({ value: v, label: `PHP ${v}` }))}
					/>
				</Field>
				<Field
					label="Profile"
					for="profile"
					hint={profile === 'dev'
						? 'Debug logging on, caches short. For building, not for visitors.'
						: 'Tuned for visitors: opcache warm, errors logged not shown.'}
				>
					<div>
						<Segmented
							bind:value={profile}
							options={[
								{ value: 'prod', label: 'Production' },
								{ value: 'dev', label: 'Development' }
							]}
						/>
					</div>
				</Field>
				<div class="sm:col-span-2">
					<Toggle
						bind:checked={pageCache}
						label="Page cache at the edge"
						hint="Anonymous page views are served from the edge and purged automatically when content changes."
					/>
				</div>
			</div>
		</Panel>

		<Panel
			title="WordPress administrator"
			subtitle="The first account. Sign in later with one click from the site page."
		>
			<div class="grid gap-5 sm:grid-cols-2">
				<Field label="Username" for="admin_user">
					<input id="admin_user" class="input" bind:value={adminUser} required autocomplete="off" />
				</Field>
				<Field label="Email" for="admin_email">
					<input id="admin_email" type="email" class="input" bind:value={adminEmail} required />
				</Field>
				<Field
					label="Password"
					for="admin_password"
					class="sm:col-span-2"
					hint="At least 12 characters. Stored only inside WordPress."
				>
					<div class="flex gap-2">
						<input
							id="admin_password"
							type={showPassword ? 'text' : 'password'}
							class="input font-mono"
							bind:value={adminPassword}
							required
							minlength="12"
							autocomplete="new-password"
						/>
						<Button
							onclick={() => (showPassword = !showPassword)}
							icon={showPassword ? 'eye-off' : 'eye'}
							aria-label={showPassword ? 'Hide password' : 'Show password'}
						/>
						<Button
							onclick={() => {
								adminPassword = generatePassword(20);
								showPassword = true;
							}}
						>
							Generate
						</Button>
					</div>
				</Field>
			</div>
		</Panel>

		{#if error}
			<Alert>{error}</Alert>
		{/if}

		<div class="flex items-center justify-end gap-2">
			<Button href={resolve('/')}>Cancel</Button>
			<Button type="submit" variant="primary" busy={submitting} disabled={!runtime}>
				Create site
			</Button>
		</div>
	</div>

	<aside class="flex flex-col gap-5">
		<Panel title="Address">
			<p class="font-mono text-sm break-all text-ink">{previewUrl}</p>
			<p class="mt-2 text-xs text-ink-2">
				{#if isLocal}
					Opens from this machine only.
				{:else}
					Public once DNS points here.
				{/if}
			</p>
		</Panel>
		<Panel title="What happens next">
			<ol class="flex flex-col gap-3">
				{#each steps as step, i (i)}
					<li class="flex gap-3 text-sm leading-5 text-ink-2">
						<span class="mt-px w-4 shrink-0 display text-xs text-ink-3 num">{i + 1}</span>
						<span>{step}</span>
					</li>
				{/each}
			</ol>
			<p class="mt-4 flex items-start gap-2 border-t border-rule pt-3 text-xs text-ink-3">
				<Icon name="camera" size={14} class="mt-0.5" />
				A preview of the front page is captured at the end and shown on the wall.
			</p>
		</Panel>
	</aside>
</form>
