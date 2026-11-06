CREATE TABLE moderation_audit_entries (
    id             UUID        PRIMARY KEY,
    server_id      UUID        NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    action         TEXT        NOT NULL,
    actor_user_id  TEXT        NOT NULL,
    target_user_id TEXT        NOT NULL,
    occurred_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_moderation_audit_server_occurred
    ON moderation_audit_entries (server_id, occurred_at DESC, id DESC);
