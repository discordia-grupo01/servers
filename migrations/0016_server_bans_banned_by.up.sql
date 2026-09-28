-- #25 Banear miembro: quién baneó (NULL en los bans anteriores a esta
-- migración) y un índice para listar los bans de un servidor del más reciente
-- al más viejo.
ALTER TABLE server_bans ADD COLUMN banned_by TEXT;

CREATE INDEX idx_server_bans_server_banned_at ON server_bans (server_id, banned_at DESC);
