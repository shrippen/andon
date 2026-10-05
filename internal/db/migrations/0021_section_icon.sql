-- Icon spec shown before a section's title ("" = none); same specs as tile icons.
ALTER TABLE sections ADD COLUMN icon TEXT NOT NULL DEFAULT '';
