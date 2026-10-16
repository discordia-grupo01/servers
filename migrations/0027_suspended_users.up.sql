-- Réplica de las suspensiones a nivel plataforma, alimentada por los eventos
-- admin.user_suspended y admin.user_reactivated de admin. admin es la fuente
-- de verdad: version es su suspension_version y solo se aplica una versión
-- mayor a la guardada. Una fila por usuario que alguna vez fue suspendido;
-- tras reactivar queda suspended = FALSE para recordar la version.
-- Sin FK a server_members: admin puede suspender a alguien que servers todavía
-- no conoce.
CREATE TABLE suspended_users (
    user_id    TEXT PRIMARY KEY,
    suspended  BOOLEAN NOT NULL,
    version    BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
