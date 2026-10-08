CREATE TABLE server_mutes (
    server_id UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL,
    muted_by  TEXT NOT NULL,
    muted_at  TIMESTAMPTZ NOT NULL,
    until_at  TIMESTAMPTZ NOT NULL,
    reason    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (server_id, user_id),
    CHECK (until_at > muted_at)
);

CREATE INDEX idx_server_mutes_until_at ON server_mutes (until_at);
