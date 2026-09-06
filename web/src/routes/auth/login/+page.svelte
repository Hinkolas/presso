<script lang="ts">
	import { enhance } from '$app/forms';
	import type { ActionData, PageData } from './$types';

	let { form, data }: { form: ActionData; data: PageData } = $props();
	let submitting = $state(false);
</script>

<svelte:head>
	<title>Sign in</title>
</svelte:head>

<!-- Unstyled on purpose: the design lands later. Keep the form semantics. -->
<main class="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-4 p-6">
	<h1 class="text-xl font-semibold">Sign in</h1>

	<form
		method="post"
		use:enhance={() => {
			submitting = true;
			return async ({ update }) => {
				await update();
				submitting = false;
			};
		}}
		class="flex flex-col gap-3"
	>
		<input type="hidden" name="next" value={data.next} />
		<label class="flex flex-col gap-1">
			<span>Email</span>
			<input
				type="email"
				name="email"
				required
				autocomplete="username"
				value={form?.email ?? ''}
				class="rounded border px-3 py-2"
			/>
		</label>
		<label class="flex flex-col gap-1">
			<span>Password</span>
			<input
				type="password"
				name="password"
				required
				autocomplete="current-password"
				class="rounded border px-3 py-2"
			/>
		</label>

		{#if form?.message}
			<p class="text-red-700" role="alert">{form.message}</p>
		{/if}

		<button
			type="submit"
			disabled={submitting}
			class="rounded border px-3 py-2 disabled:opacity-60"
		>
			{submitting ? 'Signing in…' : 'Sign in'}
		</button>
	</form>
</main>
