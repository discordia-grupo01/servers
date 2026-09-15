-- "categorias nativas": todo server tiene, desde que se crea, una categoria
-- para canales de texto y otra para canales de voz. Un canal nuevo sin
-- category_id explicito cae en la nativa que matchea su tipo (ver
-- channel_service.CreateChannel). A diferencia de las categorias que crea
-- el owner, estas se pueden renombrar y borrar igual que cualquier otra: el
-- tag "es la nativa de texto/voz de este server" vive en default_kind, no en
-- el nombre, asi que sobrevive un rename; si se borra, un canal nuevo de ese
-- tipo simplemente vuelve a caer en NULL ("sin categoria").
ALTER TABLE categories
    ADD COLUMN default_kind VARCHAR(10) CHECK (default_kind IN ('text', 'voice'));

-- A lo sumo una categoria por server puede ser la nativa de cada tipo.
CREATE UNIQUE INDEX uq_categories_server_default_kind
    ON categories (server_id, default_kind) WHERE default_kind IS NOT NULL;

-- Backfill: los servers que ya existen no tienen estas categorias todavia.
-- Se insertan con position negativa para que ordenen antes que cualquier
-- categoria que el owner ya haya creado (sin tener que re-numerar nada).
INSERT INTO categories (id, server_id, name, position, default_kind, created_at)
SELECT gen_random_uuid(), s.id, 'Canales de texto', -2, 'text', now()
  FROM servers s
 WHERE NOT EXISTS (
     SELECT 1 FROM categories c WHERE c.server_id = s.id AND c.default_kind = 'text'
 );

INSERT INTO categories (id, server_id, name, position, default_kind, created_at)
SELECT gen_random_uuid(), s.id, 'Canales de voz', -1, 'voice', now()
  FROM servers s
 WHERE NOT EXISTS (
     SELECT 1 FROM categories c WHERE c.server_id = s.id AND c.default_kind = 'voice'
 );

-- Backfill: los canales que hoy estan en "sin categoria" pasan a la nativa
-- que matchea su tipo.
UPDATE channels ch
   SET category_id = cat.id
  FROM categories cat
 WHERE ch.category_id IS NULL
   AND cat.server_id = ch.server_id
   AND cat.default_kind = ch.kind;
