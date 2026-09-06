<!-- Account security: the password, and every session signed in as you. -->
<script lang="ts">
	import { api, errorText } from '$lib/api/client';
	import { session } from '$lib/state/session.svelte';
	import { deviceName, isBrowser } from '$lib/device';
	import { formatDate, formatDateTime, formatRelative } from '$lib/format';
	import type { AuthSession, SessionsResponse } from '$lib/types/auth';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Field from '$lib/components/Field.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Table, { td, th } from '$lib/components/Table.svelte';

	// Password dialog.
	let pwOpen = $state(false);
	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let pwBusy = $state(false);
	let pwError = $state('');
	let pwChangedAt = $state<number | null>(null);

	function openPassword() {
		current = next = confirm = '';
		pwError = '';
		pwOpen = true;
	}

	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		pwError = '';
		if (next !== confirm) {
			pwError = 'The new passwords do not match. Type the same password in both fields.';
			return;
		}
		pwBusy = true;
		try {
			await api.post('/v1/auth/password', { current_password: current, new_password: next });
			pwOpen = false;
			pwChangedAt = Date.now();
			// Every other session was revoked by the change.
			await loadSessions();
		} catch (err) {
			pwError = errorText(err);
		} finally {
			pwBusy = false;
		}
	}

	// Sessions.
	let sessions = $state<AuthSession[] | null>(null);
	let sessError = $state('');
	let revoking = $state<string | null>(null);

	async function loadSessions() {
		try {
			const res = await api.get<SessionsResponse>('/v1/auth/sessions');
			// Current first, then newest.
			sessions = res.sessions.sort(
				(a, b) => Number(b.current) - Number(a.current) || b.created_at.localeCompare(a.created_at)
			);
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

	const others = $derived((sessions ?? []).filter((s) => !s.current).length);
	async function revokeOthers() {
		for (const s of sessions ?? []) if (!s.current) await revoke(s.id);
	}

	const email = $derived(session.user?.email ?? '');
</script>

<svelte:head>
	<title>Account security · presso</title>
</svelte:head>

<PageHeader
	title="Account security"
	subtitle="Password and active sessions for {email || 'your account'}."
/>

<div class="flex max-w-5xl flex-col gap-8">
	{#if pwChangedAt}
		<Alert tone="success">Password changed. Every other session was signed out.</Alert>
	{/if}

	<section class="flex items-center gap-5 frame px-6 py-5">
		<span class="grid size-12 shrink-0 place-items-center rounded-control bg-sunken text-ink-2">
			<Icon name="key" size={22} />
		</span>
		<div class="min-w-0 flex-1">
			<h2 class="text-base font-semibold text-ink">Password</h2>
			<p class="mt-0.5 text-sm text-ink-2">
				Changing it signs out every other session; only this one stays signed in.
			</p>
		</div>
		<Button onclick={openPassword}>Change password</Button>
	</section>

	<section class="flex flex-col gap-4">
		<div class="flex items-end justify-between gap-4">
			<div>
				<h2 class="display text-2xl text-ink">Active sessions</h2>
				<p class="mt-1 text-sm text-ink-2">Every browser or client signed in as you.</p>
			</div>
			{#if others > 0}
				<Button size="sm" onclick={revokeOthers} disabled={revoking !== null}>
					Sign out {others === 1 ? 'the other session' : `${others} other sessions`}
				</Button>
			{/if}
		</div>

		{#if sessError}
			<Alert>{sessError}</Alert>
		{/if}

		<Table>
			<thead>
				<tr>
					<th class={th}>Device</th>
					<th class={th}>IP address</th>
					<th class={th}>Signed in</th>
					<th class={th}>Expires</th>
					<th class={th}><span class="sr-only">Sign out</span></th>
				</tr>
			</thead>
			<tbody>
				{#if sessions === null}
					{#each [0, 1] as i (i)}
						<tr aria-busy="true">
							<td class={td}><div class="h-5 w-48 animate-pulse rounded bg-sunken"></div></td>
							<td class={td}><div class="h-5 w-24 animate-pulse rounded bg-sunken"></div></td>
							<td class={td}><div class="h-5 w-20 animate-pulse rounded bg-sunken"></div></td>
							<td class={td}><div class="h-5 w-28 animate-pulse rounded bg-sunken"></div></td>
							<td class={td}></td>
						</tr>
					{/each}
				{:else if sessions.length === 0}
					<tr>
						<td class="{td} text-ink-3" colspan="5">No sessions found.</td>
					</tr>
				{:else}
					{#each sessions as s (s.id)}
						<tr>
							<td class={td}>
								<div class="flex items-center gap-3">
									<span
										class="grid size-10 shrink-0 place-items-center rounded-control-sm bg-sunken text-ink-2"
									>
										<Icon name={isBrowser(s.user_agent) ? 'monitor' : 'terminal'} size={18} />
									</span>
									<span class="font-medium text-ink" title={s.user_agent}
										>{deviceName(s.user_agent)}</span
									>
									{#if s.current}
										<span
											class="rounded-full bg-brand-soft px-2.5 py-0.5 text-2xs font-semibold text-brand"
											>This device</span
										>
									{/if}
								</div>
							</td>
							<td class="{td} font-mono text-xs text-ink-2">{s.ip_address || '–'}</td>
							<td class="{td} text-ink-2 num" title={formatDateTime(s.created_at)}>
								{formatRelative(s.created_at)}
							</td>
							<td class="{td} text-ink-2 num" title={formatDateTime(s.expires_at)}>
								{formatDate(s.expires_at)}
							</td>
							<td class="{td} w-16 text-right">
								{#if !s.current}
									<button
										type="button"
										class="inline-flex size-9 items-center justify-center rounded-control-sm text-ink-3 hover:bg-danger-soft hover:text-danger-ink disabled:opacity-50"
										aria-label="Sign out {deviceName(s.user_agent)}"
										title="Sign out this session"
										disabled={revoking !== null}
										onclick={() => revoke(s.id)}
									>
										<Icon name="close" size={16} />
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				{/if}
			</tbody>
		</Table>
	</section>
</div>

<Modal bind:open={pwOpen} title="Change password">
	<form id="pw-form" onsubmit={changePassword} class="flex flex-col gap-5">
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
		<Field label="New password" for="pw-next" hint="At least 12 characters.">
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
		<Field label="Repeat new password" for="pw-confirm">
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
	</form>
	{#snippet footer()}
		<Button onclick={() => (pwOpen = false)}>Cancel</Button>
		<Button type="submit" form="pw-form" variant="primary" busy={pwBusy}>Change password</Button>
	{/snippet}
</Modal>
