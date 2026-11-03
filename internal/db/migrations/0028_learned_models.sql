-- A learned model (services/learn) per purpose, connection and holder
-- (0 for a shared login, else whose login: user id, -team id): the
-- examples it learned as JSON (bayes.Model), kept when the data they
-- came from is gone, so it does not start over.
CREATE TABLE learned_models (
    purpose TEXT NOT NULL,
    conn_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    holder INTEGER NOT NULL DEFAULT 0,
    model TEXT NOT NULL DEFAULT '{}',
    trained_at TEXT NOT NULL,
    PRIMARY KEY (purpose, conn_id, holder)
);
