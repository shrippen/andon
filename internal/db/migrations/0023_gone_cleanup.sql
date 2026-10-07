-- Revisions and shares point at boards, widgets, themes, connections,
-- spaces, users and teams by kind and id, without a foreign key. SQLite
-- hands a deleted row's id to the next row, which then inherited them:
-- a new board showed the old one's history, a new team its shares.
-- Delete them with their target, and drop what is already orphaned.

CREATE TRIGGER boards_gone AFTER DELETE ON boards BEGIN
    DELETE FROM revisions WHERE kind = 'board' AND entity_id = OLD.id;
    DELETE FROM shares WHERE resource_kind = 'board' AND resource_id = OLD.id;
END;

CREATE TRIGGER widgets_gone AFTER DELETE ON widgets BEGIN
    DELETE FROM revisions WHERE kind = 'widget' AND entity_id = OLD.id;
    DELETE FROM shares WHERE resource_kind = 'widget' AND resource_id = OLD.id;
END;

CREATE TRIGGER themes_gone AFTER DELETE ON themes BEGIN
    DELETE FROM revisions WHERE kind = 'theme' AND entity_id = OLD.id;
    DELETE FROM shares WHERE resource_kind = 'theme' AND resource_id = OLD.id;
END;

CREATE TRIGGER connections_gone AFTER DELETE ON connections BEGIN
    DELETE FROM shares WHERE resource_kind = 'connection' AND resource_id = OLD.id;
END;

CREATE TRIGGER spaces_gone AFTER DELETE ON spaces BEGIN
    DELETE FROM shares WHERE resource_kind = 'space' AND resource_id = OLD.id;
END;

CREATE TRIGGER users_gone AFTER DELETE ON users BEGIN
    DELETE FROM shares WHERE grantee_kind = 'user' AND grantee_id = OLD.id;
END;

CREATE TRIGGER teams_gone AFTER DELETE ON teams BEGIN
    DELETE FROM shares WHERE grantee_kind = 'team' AND grantee_id = OLD.id;
END;

DELETE FROM revisions WHERE kind = 'board' AND entity_id NOT IN (SELECT id FROM boards);
DELETE FROM revisions WHERE kind = 'widget' AND entity_id NOT IN (SELECT id FROM widgets);
DELETE FROM revisions WHERE kind = 'theme' AND entity_id NOT IN (SELECT id FROM themes);
DELETE FROM shares WHERE resource_kind = 'board' AND resource_id NOT IN (SELECT id FROM boards);
DELETE FROM shares WHERE resource_kind = 'widget' AND resource_id NOT IN (SELECT id FROM widgets);
DELETE FROM shares WHERE resource_kind = 'theme' AND resource_id NOT IN (SELECT id FROM themes);
DELETE FROM shares WHERE resource_kind = 'connection' AND resource_id NOT IN (SELECT id FROM connections);
DELETE FROM shares WHERE resource_kind = 'space' AND resource_id NOT IN (SELECT id FROM spaces);
DELETE FROM shares WHERE grantee_kind = 'user' AND grantee_id NOT IN (SELECT id FROM users);
DELETE FROM shares WHERE grantee_kind = 'team' AND grantee_id NOT IN (SELECT id FROM teams);
