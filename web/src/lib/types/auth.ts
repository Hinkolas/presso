// Shapes mirror the engine's JSON payloads (internal/api/auth_handlers.go).

export type AuthUser = {
	id: string;
	email: string;
	name: string;
	role: 'admin' | 'member';
	created_at: string;
};

export type AuthSession = {
	id: string;
	expires_at: string;
	ip_address: string;
	user_agent: string;
	created_at: string;
	current: boolean;
};

export type SessionResponse = { user: AuthUser; session: AuthSession };
export type SessionsResponse = { sessions: AuthSession[] };
export type LoginResponse = { session: { token: string; expires_at: string; user: AuthUser } };
