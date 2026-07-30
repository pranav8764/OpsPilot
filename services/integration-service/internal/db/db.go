package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 2
	// Retry until the caller's context expires. A cold start races postgres
	// coming up, and previously the first failure exited the process, which
	// dropped it into Docker's restart backoff -- turning a two-second race
	// into up to a minute of downtime and a cascade through depends_on.
	var lastErr error
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			lastErr = fmt.Errorf("create pool: %w", err)
		} else if err := pool.Ping(ctx); err != nil {
			pool.Close()
			lastErr = fmt.Errorf("ping db: %w", err)
		} else {
			return pool, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("database unreachable after %d attempts: %w", attempt, lastErr)
		case <-time.After(2 * time.Second):
		}
	}
}
