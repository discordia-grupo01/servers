-- Rol @everyone: todo miembro de un servidor lo tiene siempre, ademas del rol
-- default de la historia #24 (independiente, sigue existiendo aparte). No se
-- puede borrar, renombrar ni recolorear (ver guardas en role_service y
-- member_role_service), y solo el owner puede editar sus permisos --
-- position = 0 ya alcanza para eso via CanManageRole, sin agregar ningun
-- bypass ahi (ver model/role/standing.go).
ALTER TABLE roles ADD COLUMN is_everyone BOOLEAN NOT NULL DEFAULT false;

-- Como mucho un @everyone por servidor.
CREATE UNIQUE INDEX uq_roles_server_everyone ON roles (server_id) WHERE is_everyone;

-- position = 0 es un valor reservado que ningun rol comun usa (arrancan en 1,
-- ver 0015_role_positions)
ALTER TABLE roles DROP CONSTRAINT roles_position_positive;
ALTER TABLE roles ADD CONSTRAINT roles_position_non_negative CHECK (position >= 0);

-- Backfill 1/2: crear @everyone para cada servidor que todavia no lo tiene.
-- permissions = DefaultPermissions (VIEW_CHANNELS=1 | SEND_MESSAGES=2 = 3)
-- | CREATE_INVITE (256) = 259
INSERT INTO roles (id, server_id, name, color, permissions, position, is_everyone, created_at, updated_at)
SELECT gen_random_uuid(), s.id, '@everyone', '#9e9e9e', 259, 0, true, now(), now()
  FROM servers s
 WHERE NOT EXISTS (SELECT 1 FROM roles r WHERE r.server_id = s.id AND r.is_everyone);

-- Backfill 2/2: asignar @everyone a todos los miembros ya existentes
-- (owner incluido), de cada servidor.
INSERT INTO member_roles (server_id, user_id, role_id, assigned_at)
SELECT sm.server_id, sm.user_id, r.id, now()
  FROM server_members sm
  JOIN roles r ON r.server_id = sm.server_id AND r.is_everyone
 ON CONFLICT (server_id, user_id, role_id) DO NOTHING;
