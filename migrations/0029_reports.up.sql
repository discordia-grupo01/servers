-- Reportes de mensajes y de usuarios que hacen los miembros de un servidor para
-- que los moderadores los revisen. Un reporte de mensaje guarda el canal y el
-- mensaje (messaging es el dueño del contenido); uno de usuario guarda el
-- usuario reportado. El estado arranca en pending y pasa una sola vez a
-- resolved o dismissed, con quién y cuándo lo cerró.
--
-- Los dos índices únicos parciales son el "reporte duplicado": un miembro no
-- puede tener dos reportes pendientes sobre el mismo mensaje o usuario, pero
-- puede volver a reportarlo cuando el anterior ya se cerró.
CREATE TABLE reports (
    id               UUID        PRIMARY KEY,
    server_id        UUID        NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    kind             TEXT        NOT NULL,
    reporter_id      TEXT        NOT NULL,
    channel_id       UUID        NULL REFERENCES channels (id) ON DELETE CASCADE,
    message_id       UUID        NULL,
    reported_user_id TEXT        NULL,
    reason           TEXT        NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending',
    created_at       TIMESTAMPTZ NOT NULL,
    resolved_at      TIMESTAMPTZ NULL,
    resolved_by      TEXT        NULL,
    CHECK (kind IN ('message', 'user')),
    CHECK (status IN ('pending', 'resolved', 'dismissed')),
    CHECK (reason <> '' AND char_length(reason) <= 512),
    CHECK (
        (kind = 'message' AND channel_id IS NOT NULL AND message_id IS NOT NULL AND reported_user_id IS NULL)
        OR (kind = 'user' AND reported_user_id IS NOT NULL AND channel_id IS NULL AND message_id IS NULL)
    ),
    CHECK (
        (status = 'pending' AND resolved_at IS NULL AND resolved_by IS NULL)
        OR (status <> 'pending' AND resolved_at IS NOT NULL AND resolved_by IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_reports_pending_message
    ON reports (server_id, reporter_id, message_id)
    WHERE status = 'pending' AND kind = 'message';

CREATE UNIQUE INDEX uq_reports_pending_user
    ON reports (server_id, reporter_id, reported_user_id)
    WHERE status = 'pending' AND kind = 'user';

CREATE INDEX idx_reports_server_status_created
    ON reports (server_id, status, created_at DESC);
