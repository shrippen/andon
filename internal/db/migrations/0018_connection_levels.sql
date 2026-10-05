-- Connections on three levels (instance, team, personal), each fixed (one
-- login on the connection) or a template (one login per holder). A holder
-- is a user, or a team that activated an instance template for itself.
-- A template's revision grows with every edit; a login remembers the
-- revision and the template's values it was entered for, so an edit
-- pauses it and the person sees what changed.
ALTER TABLE connections ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

CREATE TABLE credentials (
    id INTEGER PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    secret_enc BLOB,
    secret_at TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    snapshot TEXT NOT NULL DEFAULT '{}',
    CHECK ((user_id IS NULL) <> (team_id IS NULL))
);
CREATE UNIQUE INDEX credentials_user ON credentials (connection_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX credentials_team ON credentials (connection_id, team_id) WHERE team_id IS NOT NULL;

INSERT INTO credentials (connection_id, user_id, secret_enc, secret_at)
    SELECT connection_id, user_id, secret_enc, secret_at FROM user_credentials;
DROP TABLE user_credentials;

-- A personal space holds only fixed connections: its owner's own login
-- becomes the connection's, everybody else's goes.
CREATE TEMP TABLE own_templates AS
    SELECT c.id AS connection_id, s.owner_user_id AS owner_id
    FROM connections c JOIN spaces s ON s.id = c.space_id
    WHERE s.kind = 'personal' AND c.credential_mode = 'personal';

UPDATE connections SET
    secret_enc = (SELECT cr.secret_enc FROM credentials cr JOIN own_templates o ON o.connection_id = cr.connection_id
                  WHERE cr.connection_id = connections.id AND cr.user_id = o.owner_id),
    secret_at = COALESCE((SELECT cr.secret_at FROM credentials cr JOIN own_templates o ON o.connection_id = cr.connection_id
                          WHERE cr.connection_id = connections.id AND cr.user_id = o.owner_id), ''),
    credential_mode = 'shared'
WHERE id IN (SELECT connection_id FROM own_templates);

DELETE FROM credentials WHERE connection_id IN (SELECT connection_id FROM own_templates);
-- A shared grant left from before the switch to personal yields to the owner's.
DELETE FROM oauth_grants WHERE user_id = 0 AND EXISTS (
    SELECT 1 FROM own_templates o JOIN oauth_grants g
        ON g.connection_id = o.connection_id AND g.user_id = o.owner_id
    WHERE o.connection_id = oauth_grants.connection_id);
UPDATE oauth_grants SET user_id = 0
    WHERE user_id = (SELECT owner_id FROM own_templates o WHERE o.connection_id = oauth_grants.connection_id);
DELETE FROM oauth_grants WHERE user_id <> 0 AND connection_id IN (SELECT connection_id FROM own_templates);
DROP TABLE own_templates;

-- Who sees a connection follows from its level alone.
DELETE FROM shares WHERE resource_kind = 'connection';
