-- When a connection last failed, so its state follows the latest fetch:
-- a successful test is no failure any more, however many came before.
ALTER TABLE conn_stats ADD COLUMN last_fail_at TEXT NOT NULL DEFAULT '';
