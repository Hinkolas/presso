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

<main class="flex min-h-screen items-center justify-center bg-gray-50 p-6">
	<div class="w-full max-w-sm rounded-lg border border-gray-200 bg-white p-6 shadow-sm">
		<div class="mb-6">
			<span class="text-lg font-bold tracking-tight text-gray-900">presso</span>
			<h1 class="mt-1 text-sm text-gray-500">Sign in to manage your sites</h1>
		</div>

		<form onsubmit={submit} class="flex flex-col gap-4">
			<label class="flex flex-col gap-1">
				<span class="text-xs font-medium text-gray-700">Email</span>
				<input
					type="email"
					name="email"
					required
					autocomplete="username"
					bind:value={email}
					class="input"
				/>
			</label>
			<label class="flex flex-col gap-1">
				<span class="text-xs font-medium text-gray-700">Password</span>
				<input
					type="password"
					name="password"
					required
					autocomplete="current-password"
					bind:value={password}
					class="input"
				/>
			</label>

			{#if message}
				<Alert>{message}</Alert>
			{/if}

			<Button type="submit" variant="primary" busy={submitting} class="w-full">
				{submitting ? 'Signing in…' : 'Sign in'}
			</Button>
		</form>
	</div>
</main>
