-- The claude.ai plan integration (undocumented api/oauth/usage with a
-- login OAuth token) is gone: Anthropic suspended the account it read.
-- Delete its connections; dependent rows go by cascade and triggers.

DELETE FROM connections WHERE service = 'claudeplan';
