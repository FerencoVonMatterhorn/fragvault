// Package db is the PostgreSQL persistence layer. It replaces the JSON-file
// store Phase 1 used — see docs/adr-002-database.md for why Postgres.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pool and waits until the database actually answers.
//
// The wait is not optional. Compose starts the backend and Postgres together,
// and Postgres needs a few seconds before it accepts connections. Without
// this the backend dies on boot and depends on the restart policy to
// eventually win the race, which looks like a flapping service rather than
// the ordinary startup ordering it is.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	// Deliberately small: Postgres shares a small VM with everything else and
	// each backend connection costs it memory. This app is not connection
	// hungry — a handful of HTTP handlers and one analysis worker.
	cfg.MaxConns = 8
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	const attempts = 30
	var lastErr error
	for i := 1; i <= attempts; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		lastErr = pool.Ping(pingCtx)
		cancel()
		if lastErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	pool.Close()
	return nil, fmt.Errorf("database still unreachable after %d attempts: %w", attempts, lastErr)
}
