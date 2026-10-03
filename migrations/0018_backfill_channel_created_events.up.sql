-- Los canales creados antes de que existiera el evento channel_created nunca
-- se anunciaron, asi que messaging no los conoce y responde CHANNEL_NOT_FOUND.
-- Se re-anuncian todos: messaging hace upsert, los que ya conoce no cambian.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.channel_created',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
           'server_id',   server_id::text,
           'channel_id',  id::text,
           'name',        name,
           'kind',        kind
       )
FROM channels;
