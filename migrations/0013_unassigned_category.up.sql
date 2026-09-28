-- "Sin asignar": el nuevo fallback unico para cualquier canal sin categoria,
-- sea de texto o de voz. 
ALTER TABLE categories
    ADD COLUMN is_fallback BOOLEAN NOT NULL DEFAULT false;

-- A lo sumo una categoria por server puede ser la de fallback.
CREATE UNIQUE INDEX uq_categories_server_is_fallback
    ON categories (server_id) WHERE is_fallback;

-- Backfill
INSERT INTO categories (id, server_id, name, name_key, position, is_fallback, created_at)
SELECT gen_random_uuid(), s.id, 'Sin asignar', 'sin asignar',
       COALESCE((SELECT MAX(position) + 1 FROM categories c WHERE c.server_id = s.id), 0),
       true, now()
  FROM servers s
 WHERE NOT EXISTS (
     SELECT 1 FROM categories c WHERE c.server_id = s.id AND c.is_fallback
 )
   AND NOT EXISTS (
     SELECT 1 FROM categories c WHERE c.server_id = s.id AND c.name_key = 'sin asignar'
 );
