-- Background status checks of link tiles per hour, for the response-time
-- trend of the last 24 hours. Kept for two days.
CREATE TABLE link_status_hour (
    widget_id INTEGER NOT NULL REFERENCES widgets(id) ON DELETE CASCADE,
    hour TEXT NOT NULL,
    ok INTEGER NOT NULL DEFAULT 0,
    fail INTEGER NOT NULL DEFAULT 0,
    ms_sum INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (widget_id, hour)
);
