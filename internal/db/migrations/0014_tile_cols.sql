-- A placed tile may span two columns of its section's grid.
ALTER TABLE placements ADD COLUMN cols INTEGER NOT NULL DEFAULT 1;
