-- Phase 1: turn the single-key-per-library model into a real video API.
--
-- api_keys: multiple revocable keys per library (with scopes), replacing the
-- single libraries.api_key_hash. The library row stays as the tenant; its
-- api_key_hash is kept for backward compatibility (the dev seed still works).
CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID PRIMARY KEY,
    library_id   TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    key_hash     TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL DEFAULT '',
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_api_keys_library ON api_keys (library_id);

-- webhook_endpoints: customer-registered URLs that receive event notifications.
-- events is the subset of event types this endpoint subscribes to; an empty
-- array means "all events".
CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id         UUID PRIMARY KEY,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL,
    events     TEXT[] NOT NULL DEFAULT '{}',
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_library ON webhook_endpoints (library_id);

-- webhook_deliveries: one row per (endpoint, event) attempt-set. The asynq task
-- carries this id and updates status/attempts/response_code as it retries, so a
-- redelivery never creates a duplicate row.
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id            UUID PRIMARY KEY,
    endpoint_id   UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    payload       JSONB NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending', -- pending | delivered | failed
    attempts      INT NOT NULL DEFAULT 0,
    response_code INT,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint ON webhook_deliveries (endpoint_id, created_at DESC);
