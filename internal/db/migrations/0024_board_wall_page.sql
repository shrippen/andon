-- Seconds the wall display shows each screen-high page of the board.
ALTER TABLE boards ADD COLUMN wall_page INTEGER NOT NULL DEFAULT 20;
