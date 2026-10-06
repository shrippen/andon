-- Verbünde: connections of different services that work together, and
-- the things that exist in several of them (a customer in Kimai and in
-- Invoice Ninja), one key per member (CAPABILITIES.md).
CREATE TABLE links (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL
);

-- One member per service: inside a Verbund the partner is unambiguous.
CREATE TABLE link_members (
    link_id INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    service TEXT NOT NULL,
    PRIMARY KEY (link_id, connection_id),
    UNIQUE (link_id, service)
);
CREATE INDEX link_members_connection ON link_members (connection_id);

CREATE TABLE link_entries (
    id INTEGER PRIMARY KEY,
    link_id INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- A member's id in an entry; key '' with state 'none' = no counterpart.
CREATE TABLE link_keys (
    entry_id INTEGER NOT NULL REFERENCES link_entries(id) ON DELETE CASCADE,
    link_id INTEGER NOT NULL,
    domain TEXT NOT NULL,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    key TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    PRIMARY KEY (entry_id, connection_id)
);
-- An id sits in at most one entry of a Verbund and domain.
CREATE UNIQUE INDEX link_keys_once ON link_keys (link_id, domain, connection_id, key) WHERE key <> '';

-- A Verbund needs two members, an entry two keys: what a deleted
-- connection leaves behind below that goes too.
CREATE TRIGGER link_members_gone AFTER DELETE ON link_members
BEGIN
    DELETE FROM links WHERE id = OLD.link_id
        AND (SELECT COUNT(*) FROM link_members WHERE link_id = OLD.link_id) < 2;
END;
CREATE TRIGGER link_keys_gone AFTER DELETE ON link_keys
BEGIN
    DELETE FROM link_entries WHERE id = OLD.entry_id
        AND (SELECT COUNT(*) FROM link_keys WHERE entry_id = OLD.entry_id) < 2;
END;
