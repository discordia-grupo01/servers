CREATE TABLE servers (
    id         UUID PRIMARY KEY,
    name       VARCHAR(100) NOT NULL,          
    name_key   TEXT NOT NULL,                  
    icon_url   TEXT,
    owner_id   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT servers_name_len CHECK (char_length(btrim(name)) BETWEEN 2 AND 100)
);

CREATE INDEX idx_servers_owner_id ON servers (owner_id);
CREATE UNIQUE INDEX uq_servers_owner_name_key ON servers (owner_id, name_key);

CREATE TABLE channels (
    id         UUID PRIMARY KEY,
    server_id  UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    kind       VARCHAR(10) NOT NULL CHECK (kind IN ('text', 'voice')),
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channels_server_id ON channels (server_id);
CREATE TABLE server_members (
    server_id UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL,
    is_owner  BOOLEAN NOT NULL DEFAULT FALSE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, user_id)
);

CREATE INDEX idx_server_members_user_id ON server_members (user_id);

CREATE UNIQUE INDEX uq_server_members_owner
    ON server_members (server_id) WHERE is_owner;
