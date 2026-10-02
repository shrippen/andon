-- The last failure of a link tile's day, e.g. "HTTP 502", for its detail
-- dialog; '' while the day has none.
ALTER TABLE link_status ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
