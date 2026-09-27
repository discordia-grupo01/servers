-- #56 Reordenar jerarquía de roles: cada rol tiene una posición dentro de su
-- servidor (1 = el más alto). Los roles existentes se numeran por antigüedad,
-- que es el orden en que el cliente ya los mostraba.
ALTER TABLE roles ADD COLUMN position INTEGER;

UPDATE roles r
   SET position = sub.rn
  FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY server_id ORDER BY created_at, id) AS rn
          FROM roles) sub
 WHERE r.id = sub.id;

ALTER TABLE roles ALTER COLUMN position SET NOT NULL;
ALTER TABLE roles ADD CONSTRAINT roles_position_positive CHECK (position > 0);

-- DEFERRABLE: reordenar reescribe varias posiciones en una misma transacción
-- y los valores intermedios pueden repetirse; la unicidad se valida al commit.
ALTER TABLE roles ADD CONSTRAINT uq_roles_server_position
    UNIQUE (server_id, position) DEFERRABLE INITIALLY DEFERRED;
