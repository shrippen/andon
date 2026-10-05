-- Teams created through OIDC groups had no space: give each its own, as
-- a team created by hand has.
INSERT INTO spaces (kind, name, team_id)
    SELECT 'team', t.name, t.id FROM teams t
    WHERE NOT EXISTS (SELECT 1 FROM spaces s WHERE s.team_id = t.id);
