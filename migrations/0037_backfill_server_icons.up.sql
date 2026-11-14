-- servers.server_updated empezo a llevar icon_url, pero las fotos que admin ya
-- tiene se publicaron sin el, asi que el backoffice no muestra los iconos de los
-- servidores existentes. Se re-anuncia la foto actual de cada servidor, ahora
-- con su icono. occurred_at es clock_timestamp(): posterior a cualquier evento
-- en vivo previo, asi que admin se queda con esta foto, que es la actual.
-- Correrla de nuevo es inofensivo: admin conserva la foto de occurred_at mas
-- nuevo de cada servidor.
--
-- Admin tiene que estar desplegado (con la columna icon_url) antes que esta
-- migracion: si no, guarda la foto sin el icono. En ese caso se vuelve a
-- ejecutar este INSERT a mano.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.server_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   s.id::text,
           'name',        s.name,
           'owner_id',    s.owner_id,
           'created_at',  to_char(s.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'icon_url',    COALESCE(s.icon_url, '')
       )
FROM servers s;
