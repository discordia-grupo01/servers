-- Los servidores creados antes de que existiera el evento server_updated nunca
-- se anunciaron, asi que admin no los lista. Se re-anuncian todos con su foto
-- actual: admin se queda con el evento de occurred_at mas nuevo de cada
-- servidor, asi que correr esta migracion de nuevo no pisa datos mas recientes.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.server_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   s.id::text,
           'name',        s.name,
           'owner_id',    s.owner_id,
           'created_at',  to_char(s.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
       )
FROM servers s;
