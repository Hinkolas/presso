-- +goose Up

-- Conventions for every table in this database:
--   * ids are UUIDv7 stored as TEXT (sortable by creation, readable in logs)
--     and mapped to uuid.UUID by per-column overrides in sqlc.yaml;
--   * timestamps are DATETIME columns written by Go in UTC through the
--     driver's sqlite time format, never by SQL defaults, so that the stored
--     text compares correctly (see store.Open);
--   * booleans are BOOLEAN (SQLite stores 0/1; sqlc maps them to bool).

-- Operator accounts for the engine UI/API. Login is by email
-- (case-insensitive). The engine is self-hosted: users are created via
-- `pressod user create`; there is no public signup endpoint.
CREATE TABLE users (
    id            TEXT     PRIMARY KEY,
    email         TEXT     NOT NULL,
    name          TEXT     NOT NULL DEFAULT '',
    role          TEXT     NOT NULL,          -- 'admin' | 'member' (meaning lives in internal/auth)
    password_hash TEXT     NOT NULL,          -- argon2id PHC string
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);
-- Every email lookup must go through lower(email) = lower(?).
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- Bearer sessions. token_hash is sha256 of the opaque token; the plaintext
-- token exists only in the login response.
CREATE TABLE sessions (
    id         TEXT     PRIMARY KEY,
    user_id    TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BLOB     NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    ip_address TEXT     NOT NULL DEFAULT '',
    user_agent TEXT     NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
