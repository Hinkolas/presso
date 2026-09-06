// The signed-in user, shared across the app shell. Set by the (app) layout
// load after GET /v1/auth/session and cleared on sign-out.
import type { AuthUser } from '$lib/types/auth';

export const session = $state<{ user: AuthUser | null }>({ user: null });

export function setUser(user: AuthUser | null): void {
	session.user = user;
}

export function isAdmin(): boolean {
	return session.user?.role === 'admin';
}
