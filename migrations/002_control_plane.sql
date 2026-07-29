ALTER TABLE config_snapshots ADD COLUMN IF NOT EXISTS actor TEXT NOT NULL DEFAULT 'system';
ALTER TABLE config_snapshots ADD COLUMN IF NOT EXISTS comment TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS config_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    current_snapshot_id BIGINT REFERENCES config_snapshots(id)
);
INSERT INTO config_state(singleton, current_snapshot_id)
VALUES (TRUE, (SELECT max(id) FROM config_snapshots))
ON CONFLICT (singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    action TEXT NOT NULL,
    actor TEXT NOT NULL,
    revision BIGINT NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_created_at_idx ON audit_events(created_at DESC);