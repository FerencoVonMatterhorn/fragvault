-- Phase 1 stored everything in one JSON file. This is the same data, plus the
-- tables Phase 2 needs for demo analysis.

CREATE TABLE players (
    steam_id          TEXT PRIMARY KEY,
    persona_name      TEXT        NOT NULL DEFAULT '',
    avatar_url        TEXT        NOT NULL DEFAULT '',
    -- Valve "game authentication code", pasted once during onboarding. Still
    -- plaintext, as it was in the JSON file; encrypting it is a known task.
    auth_code         TEXT        NOT NULL DEFAULT '',
    -- Most recent sharecode already walked to. Discovery resumes from here.
    latest_known_code TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- match_id and reservation_id are uint64 in the decoder but comfortably
-- inside int64 in practice (real match IDs are ~3.7e18 against a 9.2e18
-- ceiling), so BIGINT is honest here rather than NUMERIC gymnastics.
CREATE TABLE matches (
    share_code     TEXT PRIMARY KEY,
    steam_id       TEXT        NOT NULL REFERENCES players (steam_id) ON DELETE CASCADE,
    match_id       BIGINT      NOT NULL,
    reservation_id BIGINT      NOT NULL,
    tv_port        INTEGER     NOT NULL,
    discovered_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX matches_player_idx ON matches (steam_id, discovered_at);

-- One row per (match, parser version). The UNIQUE constraint is what stops a
-- demo being analysed twice: re-requesting an analysis finds the existing row
-- and returns it. Bumping parser_version is therefore the deliberate way to
-- force a re-run when the detectors change, rather than a cache to clear.
CREATE TABLE demo_analyses (
    id             BIGSERIAL PRIMARY KEY,
    share_code     TEXT        NOT NULL REFERENCES matches (share_code) ON DELETE CASCADE,
    parser_version INTEGER     NOT NULL,
    status         TEXT        NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed')),
    -- Populated when status = 'failed'. A demo Valve has already expired is a
    -- normal outcome here, not an exception.
    error          TEXT        NOT NULL DEFAULT '',
    demo_source    TEXT        NOT NULL DEFAULT '',
    map_name       TEXT        NOT NULL DEFAULT '',
    tick_rate      REAL        NOT NULL DEFAULT 0,
    duration_s     REAL        NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    UNIQUE (share_code, parser_version)
);

CREATE INDEX demo_analyses_pending_idx ON demo_analyses (status, created_at);

-- kind + metadata rather than a column per detector: adding "ninja defuse"
-- later should be code, not a migration.
CREATE TABLE highlights (
    id          BIGSERIAL PRIMARY KEY,
    analysis_id BIGINT  NOT NULL REFERENCES demo_analyses (id) ON DELETE CASCADE,
    steam_id    TEXT    NOT NULL,
    kind        TEXT    NOT NULL,
    round       INTEGER NOT NULL,
    start_tick  INTEGER NOT NULL,
    end_tick    INTEGER NOT NULL,
    start_s     REAL    NOT NULL,
    end_s       REAL    NOT NULL,
    score       REAL    NOT NULL DEFAULT 0,
    metadata    JSONB   NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX highlights_analysis_idx ON highlights (analysis_id);
CREATE INDEX highlights_player_idx ON highlights (steam_id, kind);
