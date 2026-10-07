-- The wall display: seconds per screen-filling set of tiles, the
-- transition between sets and its easing (Kante.wall).
ALTER TABLE boards ADD COLUMN wall_page INTEGER NOT NULL DEFAULT 20;
ALTER TABLE boards ADD COLUMN wall_turn TEXT NOT NULL DEFAULT 'cut';
ALTER TABLE boards ADD COLUMN wall_ease TEXT NOT NULL DEFAULT 'standard';
