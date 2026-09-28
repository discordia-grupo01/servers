-- default_kind (0010_native_categories) marcaba cual categoria era la nativa
-- de texto/voz, para que un canal sin category_id cayera en la de su tipo.
-- Ese fallback por tipo ya no existe (0013_unassigned_category): ahora todo
-- canal sin categoria cae siempre en "Sin asignar" (is_fallback), sea cual
-- sea su kind. "Canales de texto"/"Canales de voz" pasan a ser categorias
-- comunes -- el owner las puede elegir a mano igual que "Sin asignar" o
-- cualquier otra, pero ya no tienen ninguna marca especial.
DROP INDEX uq_categories_server_default_kind;

ALTER TABLE categories DROP COLUMN default_kind;
