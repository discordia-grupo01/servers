-- Réplica de solo lectura del perfil público de cada usuario, alimentada por
-- el evento users.profile_updated de identify, para que GET /members devuelva
-- el perfil sin llamar a identify. identify es la fuente de verdad: version es
-- su profile_version y solo se aplica una versión mayor a la guardada.
-- Sin FK a server_members: un perfil existe aunque el usuario no esté en
-- ningún servidor.
CREATE TABLE user_profiles (
    user_id      TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    avatar_url   TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    status_text  TEXT NOT NULL DEFAULT '',
    status_emoji TEXT NOT NULL DEFAULT '',
    version      BIGINT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
