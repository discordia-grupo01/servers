-- La auditoria de moderacion anterior al evento servers.moderation_action_recorded
-- nunca se anuncio, asi que el expediente de investigacion de admin no la tiene.
-- Se publica un evento por cada fila de moderation_audit_entries. Es un hecho
-- historico: occurred_at es el de la fila (no el momento de la migracion) y
-- action_id es el id de la fila, que admin usa para no duplicar. Correrla de
-- nuevo es inofensivo por eso.
--
-- No incluye al actor (actor_user_id): admin no necesita saber que moderador
-- actuo (minimizacion de datos).
--
-- Admin tiene que estar desplegado antes que esta migracion: si la cola que
-- consume el evento todavia no existe, RabbitMQ lo descarta. En ese caso se vuelve
-- a ejecutar este INSERT a mano.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.moderation_action_recorded',
       jsonb_build_object(
           'event_id',       gen_random_uuid()::text,
           'occurred_at',    to_char(a.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'action_id',      a.id::text,
           'server_id',      a.server_id::text,
           'action',         a.action,
           'target_user_id', a.target_user_id
       )
FROM moderation_audit_entries a;
