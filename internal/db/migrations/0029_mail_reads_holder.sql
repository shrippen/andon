-- A mail's marks (read, sent, no invoice) per holder and mail: a personal
-- mailbox is each holder's own (0 for a shared login, else user id or
-- -team id), and a UID alone may name another mail after the server
-- renumbers. mail is the mail's key (UID, sender, date); '' for marks
-- from before, which still match by UID alone. user_id and team_id
-- repeat the holder so the marks go with their user or team.
CREATE TABLE mail_marks (
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    holder INTEGER NOT NULL DEFAULT 0,
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    team_id INTEGER REFERENCES teams(id) ON DELETE CASCADE,
    uid INTEGER NOT NULL,
    mail TEXT NOT NULL DEFAULT '',
    data TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (connection_id, holder, uid, mail)
);
INSERT INTO mail_marks (connection_id, uid, data, created_at)
    SELECT connection_id, uid, data, created_at FROM mail_reads;
DROP TABLE mail_reads;
ALTER TABLE mail_marks RENAME TO mail_reads;
