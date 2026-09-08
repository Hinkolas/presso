<!-- Progress through the wizard, in JobPanel's visual language: done steps
     are green with a check, the current one a brand ring with a dot,
     upcoming ones numbered. Done steps are buttons that go back. Narrow
     viewports get one line of text instead. -->
<script lang="ts">
	import Icon from '../Icon.svelte';

	let {
		steps,
		current,
		onselect
	}: {
		steps: readonly { id: string; label: string }[];
		current: number;
		onselect?: (i: number) => void;
	} = $props();
</script>

{#snippet marker(state: 'done' | 'active' | 'upcoming', n: number)}
	<span
		class="grid size-6 shrink-0 place-items-center rounded-full border text-2xs font-semibold num {state ===
		'done'
			? 'border-ok bg-ok text-white'
			: state === 'active'
				? 'border-brand bg-surface text-brand'
				: 'border-rule-strong bg-surface text-ink-3'}"
	>
		{#if state === 'done'}
			<Icon name="check" size={12} />
		{:else if state === 'active'}
			<span class="size-2 rounded-full bg-brand"></span>
		{:else}
			{n}
		{/if}
	</span>
{/snippet}

<ol class="hidden items-center sm:flex" aria-label="Steps">
	{#each steps as s, i (s.id)}
		{@const state = i < current ? 'done' : i === current ? 'active' : 'upcoming'}
		<li
			class="flex items-center {i < steps.length - 1 ? 'flex-1' : ''}"
			aria-current={state === 'active' ? 'step' : undefined}
		>
			{#if state === 'done' && onselect}
				<button
					type="button"
					class="flex items-center gap-2.5 rounded-control-sm py-1 pr-2 text-sm font-medium text-ink-2 hover:text-ink"
					onclick={() => onselect(i)}
				>
					{@render marker(state, i + 1)}
					{s.label}
				</button>
			{:else}
				<span
					class="flex items-center gap-2.5 py-1 pr-2 text-sm {state === 'active'
						? 'font-medium text-ink'
						: 'text-ink-3'}"
				>
					{@render marker(state, i + 1)}
					{s.label}
				</span>
			{/if}
			{#if i < steps.length - 1}
				<span class="mx-3 h-px flex-1 {i < current ? 'bg-ok' : 'bg-rule'}"></span>
			{/if}
		</li>
	{/each}
</ol>
<p class="text-sm text-ink-2 sm:hidden" aria-live="polite">
	Step {current + 1} of {steps.length} ·
	<span class="font-medium text-ink">{steps[current]?.label}</span>
</p>
