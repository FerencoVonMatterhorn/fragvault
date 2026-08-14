-- Avatars come from the Steam Web API after parsing; the demo carries names
-- but no pictures. Empty for anyone whose profile couldn't be read, which the
-- UI falls back on rather than treating as an error.
ALTER TABLE match_players ADD COLUMN avatar_url TEXT NOT NULL DEFAULT '';
