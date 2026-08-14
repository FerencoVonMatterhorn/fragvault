package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/fragvault/fragvault/backend/internal/demos"
)

// Analysis is the state of one demo analysis, as the API reports it.
type Analysis struct {
	ShareCode  string              `json:"share_code"`
	Status     string              `json:"status"`
	Error      string              `json:"error,omitempty"`
	MapName    string              `json:"map_name,omitempty"`
	TeamAScore int                 `json:"team_a_score"`
	TeamBScore int                 `json:"team_b_score"`
	Rounds     []RoundResult       `json:"rounds"`
	Scoreboard []ScoreboardRow     `json:"scoreboard"`
	Highlights []AnalysisHighlight `json:"highlights"`
}

// RoundResult is one tick of the round strip.
type RoundResult struct {
	Number int     `json:"number"`
	Winner int     `json:"winner"`
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
}

// ScoreboardRow is one player's line. ADR and headshot percentage are
// computed here rather than stored, so they always agree with the totals
// beside them.
type ScoreboardRow struct {
	SteamID     string  `json:"steam_id"`
	Name        string  `json:"name"`
	AvatarURL   string  `json:"avatar_url,omitempty"`
	Team        int     `json:"team"`
	Kills       int     `json:"kills"`
	Deaths      int     `json:"deaths"`
	Assists     int     `json:"assists"`
	MVPs        int     `json:"mvps"`
	Damage      int     `json:"damage"`
	Headshots   int     `json:"headshots"`
	Rounds      int     `json:"rounds"`
	ADR         float64 `json:"adr"`
	HeadshotPct float64 `json:"headshot_pct"`
}

type AnalysisHighlight struct {
	SteamID  string         `json:"steam_id"`
	Kind     string         `json:"kind"`
	Round    int            `json:"round"`
	StartS   float64        `json:"start_s"`
	EndS     float64        `json:"end_s"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Enqueue queues an analysis, or re-queues one that failed.
//
// The UNIQUE (share_code, parser_version) constraint still does the main
// work: asking twice cannot start a second parse of the same demo. The one
// exception is a failed analysis, which the caller is explicitly allowed to
// retry — failures are often transient (the game coordinator was down, the
// download stalled), and without this a single bad minute would leave a
// match unanalysable forever at this parser version.
//
// A finished or in-flight analysis is left alone: re-running those is what
// bumping ParserVersion is for.
func (s *Store) Enqueue(ctx context.Context, shareCode, demoURL string) (queued bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO demo_analyses (share_code, parser_version, status, demo_url)
		VALUES ($1, $2, 'pending', $3)
		ON CONFLICT (share_code, parser_version) DO UPDATE
		SET status      = 'pending',
		    error       = '',
		    demo_url    = EXCLUDED.demo_url,
		    started_at  = NULL,
		    finished_at = NULL
		WHERE demo_analyses.status = 'failed'`,
		shareCode, demos.ParserVersion, demoURL)
	if err != nil {
		return false, fmt.Errorf("enqueueing analysis for %s: %w", shareCode, err)
	}
	// Zero rows means the conflict hit an analysis that isn't failed, so
	// nothing was changed and nothing needs to be.
	return tag.RowsAffected() == 1, nil
}

// RequeueOrphaned resets analyses left mid-flight by a restart.
//
// The worker is single and in-process, so any row still marked running at
// startup belongs to a process that is gone. Without this they would sit
// there forever: nothing claims a running row, and the retry path only
// covers failures.
func (s *Store) RequeueOrphaned(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE demo_analyses
		SET status = 'pending', started_at = NULL
		WHERE status = 'running'`)
	if err != nil {
		return 0, fmt.Errorf("requeueing orphaned analyses: %w", err)
	}
	return tag.RowsAffected(), nil
}

// GetAnalysis returns the analysis for a sharecode at the current parser
// version, or nil if none has been requested.
func (s *Store) GetAnalysis(ctx context.Context, shareCode string) (*Analysis, error) {
	var (
		id int64
		a  = &Analysis{
			ShareCode:  shareCode,
			Highlights: []AnalysisHighlight{},
			Scoreboard: []ScoreboardRow{},
			Rounds:     []RoundResult{},
		}
		mapName, errMsg *string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, status, error, map_name, team_a_score, team_b_score
		FROM demo_analyses
		WHERE share_code = $1 AND parser_version = $2`,
		shareCode, demos.ParserVersion).
		Scan(&id, &a.Status, &errMsg, &mapName, &a.TeamAScore, &a.TeamBScore)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading analysis for %s: %w", shareCode, err)
	}
	if errMsg != nil {
		a.Error = *errMsg
	}
	if mapName != nil {
		a.MapName = *mapName
	}

	rows, err := s.pool.Query(ctx, `
		SELECT steam_id, kind, round, start_s, end_s, score, metadata
		FROM highlights WHERE analysis_id = $1
		ORDER BY round, start_s, kind`, id)
	if err != nil {
		return nil, fmt.Errorf("loading highlights for %s: %w", shareCode, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			h   AnalysisHighlight
			raw []byte
		)
		if err := rows.Scan(&h.SteamID, &h.Kind, &h.Round, &h.StartS, &h.EndS, &h.Score, &raw); err != nil {
			return nil, fmt.Errorf("scanning highlight: %w", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &h.Metadata)
		}
		a.Highlights = append(a.Highlights, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating highlights for %s: %w", shareCode, err)
	}

	// Sorted by kills in SQL rather than in the client, so every consumer
	// gets scoreboard order without having to know what that is.
	playerRows, err := s.pool.Query(ctx, `
		SELECT steam_id, name, avatar_url, team, kills, deaths, assists, mvps, damage, headshots, rounds
		FROM match_players WHERE analysis_id = $1
		ORDER BY kills DESC, steam_id`, id)
	if err != nil {
		return nil, fmt.Errorf("loading scoreboard for %s: %w", shareCode, err)
	}
	defer playerRows.Close()

	for playerRows.Next() {
		var r ScoreboardRow
		if err := playerRows.Scan(&r.SteamID, &r.Name, &r.AvatarURL, &r.Team, &r.Kills, &r.Deaths,
			&r.Assists, &r.MVPs, &r.Damage, &r.Headshots, &r.Rounds); err != nil {
			return nil, fmt.Errorf("scanning scoreboard row: %w", err)
		}
		// Reuse the parser's definitions rather than repeating the arithmetic
		// — including its guards against dividing by zero.
		stat := demos.PlayerStat{Kills: r.Kills, Headshots: r.Headshots, Damage: r.Damage, Rounds: r.Rounds}
		r.ADR = stat.ADR()
		r.HeadshotPct = stat.HeadshotPct()
		a.Scoreboard = append(a.Scoreboard, r)
	}
	if err := playerRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating scoreboard for %s: %w", shareCode, err)
	}

	roundRows, err := s.pool.Query(ctx, `
		SELECT number, winner_team, start_s, end_s
		FROM match_rounds WHERE analysis_id = $1
		ORDER BY number`, id)
	if err != nil {
		return nil, fmt.Errorf("loading rounds for %s: %w", shareCode, err)
	}
	defer roundRows.Close()

	for roundRows.Next() {
		var r RoundResult
		if err := roundRows.Scan(&r.Number, &r.Winner, &r.StartS, &r.EndS); err != nil {
			return nil, fmt.Errorf("scanning round: %w", err)
		}
		a.Rounds = append(a.Rounds, r)
	}
	return a, roundRows.Err()
}

// ClaimNext takes the oldest pending job. FOR UPDATE SKIP LOCKED means a
// second worker would take a different row rather than block, which keeps
// this correct if the single-worker rule is ever relaxed.
func (s *Store) ClaimNext(ctx context.Context) (*demos.Job, error) {
	job := &demos.Job{}
	err := s.pool.QueryRow(ctx, `
		UPDATE demo_analyses
		SET status = 'running', started_at = now()
		WHERE id = (
			SELECT id FROM demo_analyses
			WHERE status = 'pending'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, share_code, demo_url`).Scan(&job.ID, &job.ShareCode, &job.DemoURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claiming next analysis: %w", err)
	}
	return job, nil
}

// Complete records a finished analysis and its highlights in one transaction,
// so an analysis is never marked done with only some of its highlights saved.
func (s *Store) Complete(ctx context.Context, id int64, res demos.Result) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning completion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, `
		UPDATE demo_analyses
		SET status = 'done', error = '', map_name = $2, tick_rate = $3, duration_s = $4,
		    team_a_score = $5, team_b_score = $6, detector_version = $7, finished_at = now()
		WHERE id = $1`,
		id, res.MapName, res.TickRate, res.Duration, res.TeamAScore, res.TeamBScore,
		demos.DetectorVersion); err != nil {
		return fmt.Errorf("marking analysis %d done: %w", id, err)
	}

	// Re-running an analysis row replaces its contents rather than adding to
	// them.
	if _, err := tx.Exec(ctx, `DELETE FROM highlights WHERE analysis_id = $1`, id); err != nil {
		return fmt.Errorf("clearing old highlights for %d: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM match_players WHERE analysis_id = $1`, id); err != nil {
		return fmt.Errorf("clearing old scoreboard for %d: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM match_rounds WHERE analysis_id = $1`, id); err != nil {
		return fmt.Errorf("clearing old rounds for %d: %w", id, err)
	}

	for _, r := range res.Rounds {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_rounds (analysis_id, number, winner_team, start_s, end_s)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (analysis_id, number) DO NOTHING`,
			id, r.Number, r.WinnerTeam, r.StartTime, r.EndTime); err != nil {
			return fmt.Errorf("inserting round %d: %w", r.Number, err)
		}
	}

	// The events themselves, so a detector change never needs the demo again.
	for _, tbl := range []string{"match_kills", "match_clutches", "match_defuses"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+tbl+` WHERE analysis_id = $1`, id); err != nil {
			return fmt.Errorf("clearing %s for %d: %w", tbl, id, err)
		}
	}

	// CopyFrom rather than a loop: a busy demo has thousands of kills, and
	// that many round trips is the difference between instant and noticeable.
	if len(res.Kills) > 0 {
		if _, err := tx.CopyFrom(ctx,
			pgx.Identifier{"match_kills"},
			[]string{"analysis_id", "round", "tick", "time_s", "killer_steam_id", "victim_steam_id",
				"killer_team", "victim_team", "is_headshot", "weapon"},
			pgx.CopyFromSlice(len(res.Kills), func(i int) ([]any, error) {
				k := res.Kills[i]
				return []any{id, k.Round, k.Tick, k.Time, k.KillerSteamID, k.VictimSteamID,
					k.KillerTeam, k.VictimTeam, k.IsHeadshot, k.Weapon}, nil
			}),
		); err != nil {
			return fmt.Errorf("inserting kills for %d: %w", id, err)
		}
	}

	for _, c := range res.Clutches {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_clutches (analysis_id, round, tick, time_s, steam_id, team, enemies_alive)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, c.Round, c.Tick, c.Time, c.PlayerSteamID, c.PlayerTeam, c.EnemiesAlive); err != nil {
			return fmt.Errorf("inserting clutch for %d: %w", id, err)
		}
	}

	for _, d := range res.Defuses {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_defuses (analysis_id, round, tick, time_s, steam_id, team, time_left_s)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, d.Round, d.Tick, d.Time, d.PlayerSteamID, d.PlayerTeam, d.TimeLeft); err != nil {
			return fmt.Errorf("inserting defuse for %d: %w", id, err)
		}
	}

	for _, p := range res.Players {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_players
				(analysis_id, steam_id, name, avatar_url, team, kills, deaths, assists, mvps, damage, headshots, rounds)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			id, p.SteamID, p.Name, p.AvatarURL, p.Team, p.Kills, p.Deaths, p.Assists, p.MVPs, p.Damage, p.Headshots, p.Rounds); err != nil {
			return fmt.Errorf("inserting scoreboard row: %w", err)
		}
	}

	for _, h := range res.Highlights {
		meta, err := json.Marshal(h.Metadata)
		if err != nil {
			return fmt.Errorf("encoding highlight metadata: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO highlights
				(analysis_id, steam_id, kind, round, start_tick, end_tick, start_s, end_s, score, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			id, h.SteamID, h.Kind, h.Round, h.StartTick, h.EndTick, h.StartS, h.EndS, h.Score, meta); err != nil {
			return fmt.Errorf("inserting highlight: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing analysis %d: %w", id, err)
	}
	return nil
}

// ClaimNextRederive takes a finished analysis whose highlights were produced
// by an older detector, and loads the events needed to recompute them.
//
// Marking detector_version as current up front means a crash mid-recompute
// leaves stale highlights rather than an infinite loop over the same row.
// Stale highlights are recoverable by bumping the version again; a worker
// spinning forever is not.
func (s *Store) ClaimNextRederive(ctx context.Context) (*demos.Rederive, error) {
	job := &demos.Rederive{}
	err := s.pool.QueryRow(ctx, `
		UPDATE demo_analyses
		SET detector_version = $1
		WHERE id = (
			SELECT id FROM demo_analyses
			WHERE status = 'done' AND detector_version < $1 AND parser_version = $2
			ORDER BY id
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, share_code`,
		demos.DetectorVersion, demos.ParserVersion).Scan(&job.ID, &job.ShareCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claiming re-derivation: %w", err)
	}

	events, err := s.loadEvents(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	job.Events = events
	return job, nil
}

// loadEvents reassembles what the parser originally produced.
func (s *Store) loadEvents(ctx context.Context, analysisID int64) (demos.Parsed, error) {
	var p demos.Parsed

	killRows, err := s.pool.Query(ctx, `
		SELECT round, tick, time_s, killer_steam_id, victim_steam_id, killer_team, victim_team, is_headshot, weapon
		FROM match_kills WHERE analysis_id = $1 ORDER BY time_s, id`, analysisID)
	if err != nil {
		return p, fmt.Errorf("loading kills: %w", err)
	}
	for killRows.Next() {
		var k demos.Kill
		if err := killRows.Scan(&k.Round, &k.Tick, &k.Time, &k.KillerSteamID, &k.VictimSteamID,
			&k.KillerTeam, &k.VictimTeam, &k.IsHeadshot, &k.Weapon); err != nil {
			killRows.Close()
			return p, fmt.Errorf("scanning kill: %w", err)
		}
		p.Kills = append(p.Kills, k)
	}
	killRows.Close()
	if err := killRows.Err(); err != nil {
		return p, fmt.Errorf("iterating kills: %w", err)
	}

	roundRows, err := s.pool.Query(ctx, `
		SELECT number, winner_team, start_s, end_s
		FROM match_rounds WHERE analysis_id = $1 ORDER BY number`, analysisID)
	if err != nil {
		return p, fmt.Errorf("loading rounds: %w", err)
	}
	for roundRows.Next() {
		var r demos.Round
		if err := roundRows.Scan(&r.Number, &r.WinnerTeam, &r.StartTime, &r.EndTime); err != nil {
			roundRows.Close()
			return p, fmt.Errorf("scanning round: %w", err)
		}
		p.Rounds = append(p.Rounds, r)
	}
	roundRows.Close()
	if err := roundRows.Err(); err != nil {
		return p, fmt.Errorf("iterating rounds: %w", err)
	}

	clutchRows, err := s.pool.Query(ctx, `
		SELECT round, tick, time_s, steam_id, team, enemies_alive
		FROM match_clutches WHERE analysis_id = $1 ORDER BY time_s, id`, analysisID)
	if err != nil {
		return p, fmt.Errorf("loading clutches: %w", err)
	}
	for clutchRows.Next() {
		var c demos.Clutch
		if err := clutchRows.Scan(&c.Round, &c.Tick, &c.Time, &c.PlayerSteamID, &c.PlayerTeam, &c.EnemiesAlive); err != nil {
			clutchRows.Close()
			return p, fmt.Errorf("scanning clutch: %w", err)
		}
		p.Clutches = append(p.Clutches, c)
	}
	clutchRows.Close()
	if err := clutchRows.Err(); err != nil {
		return p, fmt.Errorf("iterating clutches: %w", err)
	}

	defuseRows, err := s.pool.Query(ctx, `
		SELECT round, tick, time_s, steam_id, team, time_left_s
		FROM match_defuses WHERE analysis_id = $1 ORDER BY time_s, id`, analysisID)
	if err != nil {
		return p, fmt.Errorf("loading defuses: %w", err)
	}
	for defuseRows.Next() {
		var d demos.Defuse
		if err := defuseRows.Scan(&d.Round, &d.Tick, &d.Time, &d.PlayerSteamID, &d.PlayerTeam, &d.TimeLeft); err != nil {
			defuseRows.Close()
			return p, fmt.Errorf("scanning defuse: %w", err)
		}
		p.Defuses = append(p.Defuses, d)
	}
	defuseRows.Close()
	return p, defuseRows.Err()
}

// SaveHighlights replaces an analysis's highlights with freshly derived ones.
func (s *Store) SaveHighlights(ctx context.Context, id int64, highlights []demos.Highlight) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning highlight transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, `DELETE FROM highlights WHERE analysis_id = $1`, id); err != nil {
		return fmt.Errorf("clearing highlights for %d: %w", id, err)
	}
	for _, h := range highlights {
		meta, err := json.Marshal(h.Metadata)
		if err != nil {
			return fmt.Errorf("encoding highlight metadata: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO highlights
				(analysis_id, steam_id, kind, round, start_tick, end_tick, start_s, end_s, score, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			id, h.SteamID, h.Kind, h.Round, h.StartTick, h.EndTick, h.StartS, h.EndS, h.Score, meta); err != nil {
			return fmt.Errorf("inserting highlight: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing highlights for %d: %w", id, err)
	}
	return nil
}

// Fail records why an analysis didn't work. Expired demos land here, which is
// an expected outcome rather than an error condition.
func (s *Store) Fail(ctx context.Context, id int64, reason string) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE demo_analyses
		SET status = 'failed', error = $2, finished_at = now()
		WHERE id = $1`, id, reason); err != nil {
		return fmt.Errorf("marking analysis %d failed: %w", id, err)
	}
	return nil
}

// BestHighlight is one of a player's moments, with enough match context to
// be understood outside the match it came from.
type BestHighlight struct {
	ShareCode string         `json:"share_code"`
	MapName   string         `json:"map_name,omitempty"`
	Kind      string         `json:"kind"`
	Round     int            `json:"round"`
	StartS    float64        `json:"start_s"`
	EndS      float64        `json:"end_s"`
	Score     float64        `json:"score"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// BestHighlights returns a player's own moments across every analysed match,
// best first.
//
// Scoped to the player's own matches as well as their steam id: appearing in
// someone else's demo shouldn't put their match list within reach.
func (s *Store) BestHighlights(ctx context.Context, steamID string, limit int) ([]BestHighlight, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.share_code, a.map_name, h.kind, h.round, h.start_s, h.end_s, h.score, h.metadata
		FROM highlights h
		JOIN demo_analyses a ON a.id = h.analysis_id
		JOIN matches m       ON m.share_code = a.share_code
		WHERE h.steam_id = $1
		  AND m.steam_id = $1
		  AND a.parser_version = $2
		  AND a.status = 'done'
		ORDER BY h.score DESC, a.share_code, h.round
		LIMIT $3`, steamID, demos.ParserVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("loading best highlights for %s: %w", steamID, err)
	}
	defer rows.Close()

	out := []BestHighlight{}
	for rows.Next() {
		var (
			h   BestHighlight
			raw []byte
		)
		if err := rows.Scan(&h.ShareCode, &h.MapName, &h.Kind, &h.Round, &h.StartS, &h.EndS, &h.Score, &raw); err != nil {
			return nil, fmt.Errorf("scanning highlight: %w", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &h.Metadata)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// MatchBelongsTo reports whether a sharecode is one of this player's matches,
// so one user cannot queue analyses against another's match list.
func (s *Store) MatchBelongsTo(ctx context.Context, shareCode, steamID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM matches WHERE share_code = $1 AND steam_id = $2)`,
		shareCode, steamID).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking match ownership: %w", err)
	}
	return exists, nil
}
