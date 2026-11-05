-- Boosts de servidor. billing es dueño de los puntos; servers decide si el boost
-- se acepta y lleva el nivel. Cada boost se guarda por su boost_id con la
-- respuesta que se dio, así un evento repetido responde lo mismo que la primera
-- vez. Un boost cancelado antes de que llegue su pedido también deja su fila,
-- para poder rechazarlo cuando llegue.
CREATE TABLE server_boosts (
    boost_id   UUID        PRIMARY KEY,
    server_id  UUID        NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    user_id    TEXT        NOT NULL,
    status     TEXT        NOT NULL CHECK (status IN ('active', 'rejected', 'cancelled', 'removed')),
    reason     TEXT        NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status = 'rejected') = (reason IS NOT NULL))
);

CREATE INDEX idx_server_boosts_active ON server_boosts (server_id) WHERE status = 'active';

-- boost_version sube con cada cambio del contador y ordena server_boost_updated.
-- icon_animated hace falta para ocultar un ícono GIF si el servidor baja de nivel:
-- el ícono se guarda, pero no se muestra.
ALTER TABLE servers
    ADD COLUMN boost_count   INT     NOT NULL DEFAULT 0 CHECK (boost_count >= 0),
    ADD COLUMN boost_level   INT     NOT NULL DEFAULT 0 CHECK (boost_level BETWEEN 0 AND 3),
    ADD COLUMN boost_version BIGINT  NOT NULL DEFAULT 0,
    ADD COLUMN icon_animated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN vanity_code   TEXT    NULL UNIQUE;
