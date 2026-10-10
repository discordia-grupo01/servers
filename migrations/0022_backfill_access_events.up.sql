INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.owner_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   s.id::text,
           'owner_id',    s.owner_id
       )
FROM servers s;

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
                     (1024, 'MENTION_EVERYONE')
                 ) AS p(bit, name)
                WHERE r.permissions & p.bit <> 0
                ORDER BY p.name
           )),
           'is_everyone', r.is_everyone
       )
FROM roles r;

INSERT INTO outbox_events (id, routing_key, payload)
SELECT gen_random_uuid(),
       'servers.member_roles_updated',
       jsonb_build_object(
           'event_id',    gen_random_uuid()::text,
           'occurred_at', to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
           'server_id',   sm.server_id::text,
           'user_id',     sm.user_id,
           'role_ids',    COALESCE(
               (SELECT jsonb_agg(mr.role_id::text ORDER BY mr.role_id)
                  FROM member_roles mr
                 WHERE mr.server_id = sm.server_id AND mr.user_id = sm.user_id),
               '[]'::jsonb)
       )
FROM server_members sm;

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
                                  (1, 'VIEW_CHANNELS'), (2, 'SEND_MESSAGES'), (512, 'MANAGE_MESSAGES')
                              ) AS p(bit, name) WHERE o.allow & p.bit <> 0 ORDER BY p.name)),
                          'deny',    to_jsonb(ARRAY(
                              SELECT p.name FROM (VALUES
                                  (1, 'VIEW_CHANNELS'), (2, 'SEND_MESSAGES'), (512, 'MANAGE_MESSAGES')
                              ) AS p(bit, name) WHERE o.deny & p.bit <> 0 ORDER BY p.name))
                      ) ORDER BY o.role_id)
                 FROM channel_permission_overrides o
                WHERE o.channel_id = c.id)
       )
FROM channels c
WHERE EXISTS (SELECT 1 FROM channel_permission_overrides o WHERE o.channel_id = c.id);
