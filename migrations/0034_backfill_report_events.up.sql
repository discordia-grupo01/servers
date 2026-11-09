-- Los reportes anteriores al evento servers.report_updated nunca se anunciaron,
-- asi que el expediente de investigacion de admin no los tiene. Se publica la foto
-- actual de cada uno. occurred_at es clock_timestamp(): posterior a cualquier
-- evento en vivo previo, asi que admin se queda con esta foto, que es la actual.
-- Correrla de nuevo es inofensivo: admin conserva la foto con el occurred_at
-- mas nuevo de cada reporte.
--
-- No incluye reporter_id ni resolved_by (minimizacion de datos). Los campos sin
-- valor salen como null, no ausentes, igual que en los eventos en vivo.
--
-- Admin tiene que estar desplegado antes que esta migracion: si la cola que
-- consume el evento todavia no existe, RabbitMQ lo descarta. En ese caso se vuelve
-- a ejecutar este INSERT a mano.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.report_updated',
       jsonb_build_object(
           'event_id',         gen_random_uuid()::text,
           'occurred_at',      to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'report_id',        r.id::text,
           'server_id',        r.server_id::text,
           'kind',             r.kind,
           'reported_user_id', r.reported_user_id,
           'channel_id',       r.channel_id::text,
           'message_id',       r.message_id::text,
           'reason',           r.reason,
           'status',           r.status,
           'created_at',       to_char(r.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'resolved_at',      to_char(r.resolved_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
       )
FROM reports r;
