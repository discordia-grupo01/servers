-- servers.role_updated empezo a llevar el nombre del rol, pero los roles que ya
-- existian se publicaron sin el, asi que notifications no lo conoce y el push de
-- una mencion de rol dice "@rol". Se re-anuncia la foto actual de cada rol, ahora
-- con su nombre. occurred_at es clock_timestamp(): posterior a cualquier evento
-- en vivo previo, asi que los consumidores se quedan con esta foto, que es la
-- actual. Correrla de nuevo es inofensivo: cada consumidor conserva la foto de
-- occurred_at mas nuevo de cada rol.
--
-- notifications tiene que estar desplegado (con la columna name en su replica de
-- roles) antes que esta migracion: si no, guarda la foto sin el nombre. En ese
-- caso se vuelve a ejecutar este INSERT a mano.
INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.role_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   r.server_id::text,
           'role_id',     r.id::text,
           'permissions', to_jsonb(ARRAY(
               SELECT p.name
                 FROM (VALUES
                     (1,    'VIEW_CHANNELS'),
                     (2,    'SEND_MESSAGES'),
                     (4,    'MANAGE_CHANNELS'),
                     (8,    'MANAGE_ROLES'),
                     (16,   'KICK_MEMBERS'),
                     (32,   'BAN_MEMBERS'),
                     (64,   'MANAGE_SERVER'),
                     (128,  'ADMINISTRATOR'),
                     (256,  'CREATE_INVITE'),
                     (512,  'MANAGE_MESSAGES'),
                     (1024, 'MENTION_EVERYONE'),
                     (2048, 'ADD_REACTIONS'),
                     (4096, 'CONNECT')
                 ) AS p(bit, name)
                WHERE r.permissions & p.bit <> 0
                ORDER BY p.name
           )),
           'is_everyone', r.is_everyone,
           'name',        r.name
       )
FROM roles r;
