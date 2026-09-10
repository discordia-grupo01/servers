CREATE TABLE roles (
    id          UUID PRIMARY KEY,
    server_id   UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    color       VARCHAR(7) NOT NULL,
    permissions BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_roles_server_id ON roles (server_id);
