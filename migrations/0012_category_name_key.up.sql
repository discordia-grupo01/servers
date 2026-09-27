-- Categorias no tenian ningun chequeo de nombre duplicado: se podian crear
-- dos categorias identicas letra por letra en el mismo server. Mismo patron
-- que uq_servers_owner_name_key (0001_init.up.sql): "name" guarda el display
-- original (mayusculas/tildes tal cual las escribio el owner) y "name_key" es
-- la clave normalizada (sin tildes, en minuscula, espacios colapsados, ver
-- service.NameKey) contra la que se valida la unicidad.
CREATE EXTENSION IF NOT EXISTS unaccent;

ALTER TABLE categories ADD COLUMN name_key TEXT;

UPDATE categories
   SET name_key = lower(unaccent(regexp_replace(btrim(name), '\s+', ' ', 'g')));

-- Como no habia constraint, ya pueden existir duplicados de verdad (mismo
-- bug que este fix soluciona). No podemos perder categorias en un deploy, asi
-- que a todos menos el mas viejo de cada (server_id, name_key) se les
-- desambigua la clave con un sufijo -- el "name" que ve el usuario no cambia,
-- solo dejan de chocar entre si. Quedan iguales a la vista hasta que alguien
-- los renombre a mano.
WITH ranked AS (
    SELECT id, row_number() OVER (
        PARTITION BY server_id, name_key ORDER BY created_at, id
    ) AS rn
    FROM categories
)
UPDATE categories c
   SET name_key = c.name_key || '-dup' || ranked.rn
  FROM ranked
 WHERE c.id = ranked.id AND ranked.rn > 1;

ALTER TABLE categories ALTER COLUMN name_key SET NOT NULL;

CREATE UNIQUE INDEX uq_categories_server_name_key
    ON categories (server_id, name_key);
