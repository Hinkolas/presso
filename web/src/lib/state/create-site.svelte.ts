// The "New site" wizard is mounted once in the app shell; every entry point
// (the Sites page buttons, ⌘K, /sites/new) opens it through this state.
import { isAdmin } from './session.svelte';

export const createSite = $state({
	open: false,
	/** Id of the site the wizard just created; its card scrolls into view once, then clears this. */
	justCreated: null as string | null
});

export function openCreateSite(): void {
	if (!isAdmin()) return;
	createSite.open = true;
}

export function closeCreateSite(): void {
	createSite.open = false;
}
