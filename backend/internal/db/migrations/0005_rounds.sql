-- Per-round results. The parser has always collected these; they were simply
-- discarded. Stored now because the round strip — one tick per round, coloured
-- by the side that won — is how a player reads a match at a glance.
CREATE TABLE match_rounds (
    id          BIGSERIAL PRIMARY KEY,
    analysis_id BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    -- CS2 team ids: 2 terrorists, 3 counter-terrorists. 0 when a round ended
    -- without a recorded winner, which happens in aborted or corrupt demos.
    winner_team INTEGER NOT NULL DEFAULT 0,
    start_s     REAL    NOT NULL DEFAULT 0,
    end_s       REAL    NOT NULL DEFAULT 0,
    UNIQUE (analysis_id, number)
);

CREATE INDEX match_rounds_analysis_idx ON match_rounds (analysis_id, number);
