-- A workbench's unfinished work (workbench.Draft): per user, bench and
-- place (Verbund, period) the cases the user changed, with the values of
-- their form fields as JSON, so a closed page loses nothing.
CREATE TABLE bench_drafts (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bench TEXT NOT NULL,
    place TEXT NOT NULL,
    cases TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, bench, place)
);
