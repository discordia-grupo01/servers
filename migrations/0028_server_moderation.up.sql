-- Estado de moderación de plataforma de cada servidor, alimentado por los
-- eventos admin.server_suspended y admin.server_reactivated de admin. admin es
-- la fuente de verdad: moderation_version es su contador por servidor y solo se
-- aplica un evento con una versión mayor a la guardada.
--
-- suspended_at es el momento en que el staff suspendió el servidor (el
-- occurred_at del evento) y NULL si está activo: no hay una columna de estado
-- aparte, así no puede haber dos fuentes de verdad. Los servidores existentes
-- quedan activos y en versión 0; admin empieza en 1.
ALTER TABLE servers
    ADD COLUMN suspended_at       TIMESTAMPTZ NULL,
    ADD COLUMN moderation_version BIGINT      NOT NULL DEFAULT 0;
