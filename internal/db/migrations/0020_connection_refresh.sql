-- How often a connection's main query runs, in minutes; 0 = automatic
-- (the service's default, stretched for rate limits).
ALTER TABLE connections ADD COLUMN refresh_minutes INTEGER NOT NULL DEFAULT 0;
