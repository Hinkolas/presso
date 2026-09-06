<!-- Sign-in: posts credentials to the API, which sets the session cookie,
     then returns to the page the guard bounced from. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import type { Pathname } from '$app/types';
	import { api, ApiError, errorText } from '$lib/api/client';
	import { safeNext } from '$lib/safe-next';
	import { setUser } from '$lib/state/session.svelte';
	import type { LoginResponse } from '$lib/types/auth';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Wordmark from '$lib/components/Wordmark.svelte';

	const next = $derived(safeNext(page.url.searchParams.get('next')));

	let email = $state('');
	let password = $state('');
	let submitting = $state(false);
	let message = $state('');

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!email.trim() || !password) {
			message = 'Email and password are required.';
			return;
		}
		submitting = true;
		message = '';
		try {
			const res = await api.post<LoginResponse>('/v1/auth/login', {
				email: email.trim(),
				password
			});
			setUser(res.session.user);
			await goto(resolve(next as Pathname));
		} catch (err) {
			message =
				err instanceof ApiError && err.status >= 500
					? 'The server is unreachable. Try again in a moment.'
					: errorText(err);
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>Sign in · presso</title>
</svelte:head>

<div class="grid min-h-screen bg-ground text-ink lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
	<aside class="hidden flex-col justify-between bg-brand-deep p-12 text-white lg:flex">
		<div>
			<span class="block display text-[64px] leading-none">presso</span>
			<p class="mt-5 max-w-xs text-md text-on-brand/80">
				WordPress sites, hosted on your own machine.
			</p>
		</div>
		<ul class="flex gap-8 text-xs text-on-brand/70">
			<li>Docker runtime</li>
			<li>Edge cache</li>
			<li>One binary</li>
		</ul>
	</aside>

	<main class="flex items-center justify-center p-6">
		<div class="w-full max-w-md frame p-10">
			<div class="mb-8 flex flex-col gap-4">
				<Wordmark size="lg" />
				<div>
					<h1 class="display text-3xl">Sign in</h1>
					<p class="mt-1 text-sm text-ink-2">Use the operator account created on this host.</p>
				</div>
			</div>

			<form onsubmit={submit} class="flex flex-col gap-4">
				<Field label="Email" for="email">
					<input
						id="email"
						type="email"
						name="email"
						required
						autocomplete="username"
						bind:value={email}
						class="input"
					/>
				</Field>
				<Field label="Password" for="password">
					<input
						id="password"
						type="password"
						name="password"
						required
						autocomplete="current-password"
						bind:value={password}
						class="input"
					/>
				</Field>

				{#if message}
					<Alert>{message}</Alert>
				{/if}

				<Button type="submit" variant="primary" busy={submitting} class="mt-2 w-full">
					{submitting ? 'Signing in' : 'Sign in'}
				</Button>
			</form>
		</div>
	</main>
</div>
