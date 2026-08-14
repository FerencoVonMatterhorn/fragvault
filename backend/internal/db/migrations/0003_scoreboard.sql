-- The scoreboard, added in parser version 2. Analyses from version 1 keep
-- their rows and their highlights; they simply aren't what the app asks for
-- any more, so those matches present as needing a re-run.

ALTER TABLE demo_analyses ADD COLUMN team_a_score INTEGER NOT NULL DEFAULT 0;
ALTER TABLE demo_analyses ADD COLUMN team_b_score INTEGER NOT NULL DEFAULT 0;

-- Totals only, exactly as the game counted them. ADR and headshot percentage
-- are derived on read so they cannot drift from the rounds and kills they
-- were calculated from.
CREATE TABLE match_players (
    id          BIGSERIAL PRIMARY KEY,
    analysis_id BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    steam_id    TEXT    NOT NULL,
    name        TEXT    NOT NULL DEFAULT '',
    team        INTEGER NOT NULL,
    kills       INTEGER NOT NULL DEFAULT 0,
    deaths      INTEGER NOT NULL DEFAULT 0,
    assists     INTEGER NOT NULL DEFAULT 0,
    mvps        INTEGER NOT NULL DEFAULT 0,
    damage      INTEGER NOT NULL DEFAULT 0,
    headshots   INTEGER NOT NULL DEFAULT 0,
    rounds      INTEGER NOT NULL DEFAULT 0,
    UNIQUE (analysis_id, steam_id)
);

CREATE INDEX match_players_analysis_idx ON match_players (analysis_id);
