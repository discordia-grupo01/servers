CREATE TABLE ownership_transfers (
    id           UUID PRIMARY KEY,
    server_id    UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    from_user_id TEXT NOT NULL,
    to_user_id   TEXT NOT NULL,
    status       VARCHAR(10) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

CREATE INDEX idx_ownership_transfers_server_id ON ownership_transfers (server_id);

CREATE UNIQUE INDEX uq_ownership_transfers_pending_per_server
    ON ownership_transfers (server_id) WHERE status = 'pending';
    