CREATE TABLE server_banned_words (
    server_id UUID NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    word      TEXT NOT NULL,
    position  INTEGER NOT NULL,
    PRIMARY KEY (server_id, word),
    CHECK (word <> '' AND char_length(word) <= 50)
);
