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
	Scoreboard []ScoreboardRow     `json:"scoreboard"`
	Highlights []AnalysisHighlight `json:"highlights"`
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

// Enqueue queues an analysis, or returns the existing one.
//
// The UNIQUE (share_code, parser_version) constraint does the work: asking
// twice cannot start a second parse of the same demo. That is the whole
// mechanism for "don't analyse it again".
func (s *Store) Enqueue(ctx context.Context, shareCode, demoURL string) (created bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO demo_analyses (share_code, parser_version, status, demo_url)
		VALUES ($1, $2, 'pending', $3)
		ON CONFLICT (share_code, parser_version) DO NOTHING`,
		shareCode, demos.ParserVersion, demoURL)
	if err != nil {
		return false, fmt.Errorf("enqueueing analysis for %s: %w", shareCode, err)
	}
	return tag.RowsAffected() == 1, nil
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
	return a, playerRows.Err()
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
		    team_a_score = $5, team_b_score = $6, finished_at = now()
		WHERE id = $1`,
		id, res.MapName, res.TickRate, res.Duration, res.TeamAScore, res.TeamBScore); err != nil {
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
