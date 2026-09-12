CREATE TABLE categories (
    id         UUID PRIMARY KEY,
    server_id  UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_categories_server_id ON categories (server_id);

ALTER TABLE channels
    ADD COLUMN category_id UUID REFERENCES categories (id) ON DELETE SET NULL,
    ADD COLUMN topic TEXT;

-- "Duplicado en la categoria" (incluyendo el balde "sin categoria") se
-- valida con dos indices unicos parciales: uno por categoria y otro para
-- los canales sueltos, porque un indice unico comun trata cada NULL como
-- distinto y no detectaria duplicados entre canales sin categoria.
CREATE UNIQUE INDEX uq_channels_server_category_name
    ON channels (server_id, category_id, name) WHERE category_id IS NOT NULL;

CREATE UNIQUE INDEX uq_channels_server_no_category_name
    ON channels (server_id, name) WHERE category_id IS NULL;
