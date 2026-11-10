-- Que hizo el moderador al resolver un reporte: borrar el mensaje, silenciar,
-- expulsar o banear.
ALTER TABLE reports ADD COLUMN resolution_action TEXT NULL;

ALTER TABLE reports ADD CONSTRAINT reports_resolution_action_check CHECK (
    resolution_action IS NULL
    OR (status = 'resolved' AND resolution_action IN ('delete_message', 'mute', 'kick', 'ban'))
);

ALTER TABLE reports ADD CONSTRAINT reports_resolution_action_kind_check CHECK (
    resolution_action IS DISTINCT FROM 'delete_message' OR kind = 'message'
);
