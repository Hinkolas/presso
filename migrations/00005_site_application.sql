-- +goose Up
ALTER TABLE sites ADD COLUMN desired_state TEXT NOT NULL DEFAULT 'running' CHECK(desired_state IN ('running','stopped'));
ALTER TABLE sites ADD COLUMN traffic_ready BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN config_revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE sites ADD COLUMN applied_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sites ADD COLUMN applied_image TEXT NOT NULL DEFAULT '';
-- Existing rows remain unavailable until explicitly reconciled by an operator.
UPDATE sites SET desired_state = 'stopped' WHERE status = 'stopped';

-- +goose Down
ALTER TABLE sites DROP COLUMN applied_image;
ALTER TABLE sites DROP COLUMN applied_revision;
ALTER TABLE sites DROP COLUMN config_revision;
ALTER TABLE sites DROP COLUMN traffic_ready;
ALTER TABLE sites DROP COLUMN desired_state;
