<!-- New-site form; a successful POST lands on the site page where the
     provisioning job is shown live. -->
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
	import Card from '$lib/components/Card.svelte';
	import Field from '$lib/components/Field.svelte';
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

<div class="mb-5 flex items-center gap-3">
	<a href={resolve('/')} class="text-sm text-gray-500 hover:text-gray-900">← Sites</a>
	<h1 class="text-xl font-semibold">New site</h1>
</div>

<form onsubmit={submit} class="max-w-2xl">
	<div class="flex flex-col gap-4">
		<Card title="Site">
			<div class="grid gap-4 sm:grid-cols-2">
				<Field label="Name" for="name">
					<input id="name" class="input" bind:value={name} required placeholder="My Site" />
				</Field>
				<Field label="Slug" for="slug" hint="Lowercase letters, digits and hyphens.">
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
						? 'Local domain: TLS is off.'
						: 'TLS certificates are obtained automatically.'}
				>
					<input id="domain" class="input" bind:value={domain} placeholder={domainPlaceholder} />
				</Field>
				<Field label="PHP version" for="php">
					<select id="php" class="input" bind:value={phpVersion} disabled={!runtime}>
						{#if runtime}
							{#each runtime.php_versions as v (v)}
								<option value={v}>{v}</option>
							{/each}
						{:else}
							<option value="">Loading…</option>
						{/if}
					</select>
				</Field>
				<Field label="Profile" for="profile">
					<select id="profile" class="input" bind:value={profile}>
						<option value="prod">prod</option>
						<option value="dev">dev</option>
					</select>
				</Field>
				<div class="flex items-end pb-1.5">
					<Toggle bind:checked={pageCache} label="Page cache" />
				</div>
			</div>
		</Card>

		<Card title="WordPress admin">
			<div class="grid gap-4 sm:grid-cols-2">
				<Field label="Username" for="admin_user">
					<input id="admin_user" class="input" bind:value={adminUser} required />
				</Field>
				<Field label="Email" for="admin_email">
					<input id="admin_email" type="email" class="input" bind:value={adminEmail} required />
				</Field>
				<div class="sm:col-span-2">
					<Field label="Password" for="admin_password">
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
							<Button onclick={() => (showPassword = !showPassword)}>
								{showPassword ? 'Hide' : 'Show'}
							</Button>
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
			</div>
		</Card>

		{#if error}
			<Alert>{error}</Alert>
		{/if}

		<div class="flex items-center justify-end gap-2">
			<Button href="/">Cancel</Button>
			<Button type="submit" variant="primary" busy={submitting} disabled={!runtime}>
				Create site
			</Button>
		</div>
	</div>
</form>
