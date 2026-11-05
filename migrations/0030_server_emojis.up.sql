-- Emojis personalizados de cada servidor. Borrar un emoji es lógico: la fila y
-- la imagen se conservan para que los mensajes y reacciones que ya lo usan lo
-- sigan mostrando. El índice único solo mira los emojis activos, así que un
-- nombre vuelve a estar libre cuando se borra el emoji que lo usaba. Compara en
-- minúsculas para que :Party: y :party: no puedan convivir.
CREATE TABLE server_emojis (
    id         UUID        PRIMARY KEY,
    server_id  UUID        NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    animated   BOOLEAN     NOT NULL,
    image_url  TEXT        NOT NULL,
    created_by TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ NULL,
    deleted_by TEXT        NULL,
    CHECK (name ~ '^[A-Za-z0-9_]{2,32}$'),
    CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);

CREATE UNIQUE INDEX uq_server_emojis_active_name
    ON server_emojis (server_id, lower(name))
    WHERE deleted_at IS NULL;
