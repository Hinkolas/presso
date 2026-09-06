<!-- Account page: change password and manage the user's sessions. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { session } from '$lib/state/session.svelte';
	import { formatDateTime, formatRelative } from '$lib/format';
	import type { AuthSession, SessionsResponse } from '$lib/types/auth';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Field from '$lib/components/Field.svelte';

	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let pwBusy = $state(false);
	let pwError = $state('');
	let pwOk = $state(false);

	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		pwError = '';
		pwOk = false;
		if (next !== confirm) {
			pwError = 'The new passwords do not match.';
			return;
		}
		pwBusy = true;
		try {
			await api.post('/v1/auth/password', { current_password: current, new_password: next });
			pwOk = true;
			current = next = confirm = '';
		} catch (err) {
			pwError = errorText(err);
		} finally {
			pwBusy = false;
		}
	}

	let sessions = $state<AuthSession[] | null>(null);
	let sessError = $state('');
	let revoking = $state<string | null>(null);

	async function loadSessions() {
		try {
			sessions = (await api.get<SessionsResponse>('/v1/auth/sessions')).sessions;
			sessError = '';
		} catch (err) {
			sessError = errorText(err);
			sessions ??= [];
		}
	}
	$effect(() => {
		loadSessions();
	});

	async function revoke(id: string) {
		revoking = id;
		try {
			await api.del(`/v1/auth/sessions/${id}`);
			sessions = (sessions ?? []).filter((s) => s.id !== id);
		} catch (err) {
			sessError = errorText(err);
		} finally {
			revoking = null;
		}
	}
</script>

<svelte:head>
	<title>Account · presso</title>
</svelte:head>

<h1 class="mb-1 text-xl font-semibold">Account</h1>
<p class="mb-5 text-sm text-gray-500">
	{session.user?.email}
	{#if session.user}<span class="ml-1 rounded bg-gray-100 px-1.5 py-0.5 text-xs"
			>{session.user.role}</span
		>{/if}
</p>

<div class="grid max-w-4xl gap-4 lg:grid-cols-2">
	<Card title="Change password">
		<form onsubmit={changePassword} class="flex flex-col gap-3">
			<Field label="Current password" for="pw-current">
				<input
					id="pw-current"
					type="password"
					class="input"
					bind:value={current}
					required
					autocomplete="current-password"
				/>
			</Field>
			<Field label="New password" for="pw-next">
				<input
					id="pw-next"
					type="password"
					class="input"
					bind:value={next}
					required
					minlength="12"
					autocomplete="new-password"
				/>
			</Field>
			<Field label="Confirm new password" for="pw-confirm">
				<input
					id="pw-confirm"
					type="password"
					class="input"
					bind:value={confirm}
					required
					autocomplete="new-password"
				/>
			</Field>
			{#if pwError}<Alert>{pwError}</Alert>{/if}
			{#if pwOk}<Alert tone="success">Password changed.</Alert>{/if}
			<div class="flex justify-end">
				<Button type="submit" variant="primary" busy={pwBusy}>Change password</Button>
			</div>
		</form>
	</Card>

	<Card title="Sessions" padded={false}>
		{#if sessError}
			<div class="p-4"><Alert>{sessError}</Alert></div>
		{/if}
		{#if sessions === null}
			<p class="p-4 text-sm text-gray-500">Loading…</p>
		{:else}
			<ul class="divide-y divide-gray-100">
				{#each sessions as s (s.id)}
					<li class="flex items-center gap-3 px-4 py-3 text-sm">
						<div class="min-w-0 flex-1">
							<p class="truncate font-medium text-gray-900">
								{s.ip_address || 'unknown address'}
								{#if s.current}
									<span class="ml-1 rounded bg-green-50 px-1.5 py-0.5 text-xs text-green-700">
										this device
									</span>
								{/if}
							</p>
							<p class="truncate text-xs text-gray-500" title={s.user_agent}>
								{s.user_agent || 'unknown client'}
							</p>
							<p class="text-xs text-gray-400">
								signed in {formatRelative(s.created_at)} · expires {formatDateTime(s.expires_at)}
							</p>
						</div>
						{#if !s.current}
							<Button
								size="sm"
								variant="danger"
								busy={revoking === s.id}
								onclick={() => revoke(s.id)}
							>
								Revoke
							</Button>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
</div>
