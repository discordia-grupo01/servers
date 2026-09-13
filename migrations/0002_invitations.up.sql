CREATE TABLE invitations (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    server_id  UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    created_by TEXT NOT NULL,
    max_uses   INTEGER CHECK (max_uses IS NULL OR max_uses > 0),
    uses       INTEGER NOT NULL DEFAULT 0 CHECK (uses >= 0),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invitations_server_id ON invitations (server_id);

CREATE TABLE server_bans (
    server_id UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL,
    reason    TEXT,
    banned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, user_id)
);

CREATE INDEX idx_server_bans_user_id ON server_bans (user_id);
