-- Задел для persistence control plane после MVP.
CREATE TABLE IF NOT EXISTS config_snapshots (
    id BIGSERIAL PRIMARY KEY,
    version INTEGER NOT NULL,
    document JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS config_snapshots_created_at_idx ON config_snapshots (created_at DESC);