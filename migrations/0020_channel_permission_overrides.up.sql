-- #57 Sobrescribir permisos de un rol en un canal: para un rol y un canal,
-- qué permisos se ganan (allow) y cuáles se pierden (deny) solo en ese canal,
-- sin tocar roles.permissions. Las máscaras usan los mismos bits que
-- roles.permissions (ver internal/model/role/permission.go).
CREATE TABLE channel_permission_overrides (
    channel_id UUID        NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    role_id    UUID        NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    allow      BIGINT      NOT NULL DEFAULT 0,
    deny       BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, role_id),
    -- Un permiso no puede estar permitido y denegado a la vez.
    CONSTRAINT overrides_disjoint CHECK (allow & deny = 0),
    -- Un override vacío es lo mismo que no tenerlo: se borra en vez de guardarse.
    CONSTRAINT overrides_not_empty CHECK (allow | deny <> 0),
    -- Solo los permisos de alcance de canal (role.ChannelScopedPermissions):
    -- VIEW_CHANNELS (1) | SEND_MESSAGES (2) | MANAGE_MESSAGES (512) = 515.
    CONSTRAINT overrides_channel_scoped CHECK ((allow | deny) & ~515 = 0)
);

-- Para borrar en cascada al eliminar un rol y para juntar los overrides de los
-- roles de un usuario sin recorrer toda la tabla.
CREATE INDEX idx_overrides_role_id ON channel_permission_overrides (role_id);
