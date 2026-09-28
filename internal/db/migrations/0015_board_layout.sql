-- A board lays its sections out in the grid ('') or in flowing columns.
ALTER TABLE boards ADD COLUMN layout TEXT NOT NULL DEFAULT '';
