-- The normalized events a demo yielded, kept so detector changes never need
-- the demo again.
--
-- Valve expires matchmaking demos after a few weeks. Until now, improving a
-- detector meant re-downloading and re-parsing every demo, which silently
-- meant "every demo that still exists" — anything older was lost to future
-- improvements even though we had already read it. The demo is the scarce
-- resource; CPU is not.

CREATE TABLE match_kills (
    id              BIGSERIAL PRIMARY KEY,
    analysis_id     BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    round           INTEGER NOT NULL,
    tick            INTEGER NOT NULL,
    time_s          REAL    NOT NULL,
    killer_steam_id TEXT    NOT NULL DEFAULT '',
    victim_steam_id TEXT    NOT NULL DEFAULT '',
    killer_team     INTEGER NOT NULL DEFAULT 0,
    victim_team     INTEGER NOT NULL DEFAULT 0,
    is_headshot     BOOLEAN NOT NULL DEFAULT false,
    weapon          TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX match_kills_analysis_idx ON match_kills (analysis_id, round);

CREATE TABLE match_clutches (
    id            BIGSERIAL PRIMARY KEY,
    analysis_id   BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    round         INTEGER NOT NULL,
    tick          INTEGER NOT NULL,
    time_s        REAL    NOT NULL,
    steam_id      TEXT    NOT NULL,
    team          INTEGER NOT NULL DEFAULT 0,
    enemies_alive INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX match_clutches_analysis_idx ON match_clutches (analysis_id);

CREATE TABLE match_defuses (
    id          BIGSERIAL PRIMARY KEY,
    analysis_id BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    round       INTEGER NOT NULL,
    tick        INTEGER NOT NULL,
    time_s      REAL    NOT NULL,
    steam_id    TEXT    NOT NULL,
    team        INTEGER NOT NULL DEFAULT 0,
    time_left_s REAL    NOT NULL DEFAULT 0
);

CREATE INDEX match_defuses_analysis_idx ON match_defuses (analysis_id);

-- Split from parser_version. The parser version says how the events were
-- extracted and only changes when the demo must be read again; the detector
-- version says how highlights were derived from them, and a change there is
-- satisfied by recomputing from what is already stored.
ALTER TABLE demo_analyses ADD COLUMN detector_version INTEGER NOT NULL DEFAULT 0;

-- Finds analyses whose highlights are stale but whose events are not.
CREATE INDEX demo_analyses_rederive_idx ON demo_analyses (status, detector_version);
