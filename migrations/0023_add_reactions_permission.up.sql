ALTER TABLE channel_permission_overrides DROP CONSTRAINT overrides_channel_scoped;
ALTER TABLE channel_permission_overrides
    ADD CONSTRAINT overrides_channel_scoped CHECK ((allow | deny) & ~2563 = 0);

UPDATE roles SET permissions = permissions | 2048 WHERE permissions & 2 <> 0;

UPDATE channel_permission_overrides
   SET allow = allow | CASE WHEN allow & 2 <> 0 THEN 2048 ELSE 0 END,
       deny  = deny  | CASE WHEN deny  & 2 <> 0 THEN 2048 ELSE 0 END
 WHERE (allow | deny) & 2 <> 0;

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
                     (2048, 'ADD_REACTIONS')
                 ) AS p(bit, name)
                WHERE r.permissions & p.bit <> 0
                ORDER BY p.name
           )),
           'is_everyone', r.is_everyone
       )
FROM roles r
WHERE r.permissions & 2048 <> 0;

INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.channel_overrides_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   c.server_id::text,
           'channel_id',  c.id::text,
           'overrides',   (
               SELECT jsonb_agg(jsonb_build_object(
                          'role_id', o.role_id::text,
                          'allow',   to_jsonb(ARRAY(
                              SELECT p.name FROM (VALUES
                                  (1, 'VIEW_CHANNELS'), (2, 'SEND_MESSAGES'),
                                  (512, 'MANAGE_MESSAGES'), (2048, 'ADD_REACTIONS')
                              ) AS p(bit, name) WHERE o.allow & p.bit <> 0 ORDER BY p.name)),
                          'deny',    to_jsonb(ARRAY(
                              SELECT p.name FROM (VALUES
                                  (1, 'VIEW_CHANNELS'), (2, 'SEND_MESSAGES'),
                                  (512, 'MANAGE_MESSAGES'), (2048, 'ADD_REACTIONS')
                              ) AS p(bit, name) WHERE o.deny & p.bit <> 0 ORDER BY p.name))
                      ) ORDER BY o.role_id)
                 FROM channel_permission_overrides o
                WHERE o.channel_id = c.id)
       )
FROM channels c
WHERE EXISTS (SELECT 1 FROM channel_permission_overrides o
               WHERE o.channel_id = c.id AND (o.allow | o.deny) & 2048 <> 0);
