// Package postgres provides a PostgreSQL implementation of Dispatchr storage.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const connectTimeout = 5 * time.Second

// NewPool creates and verifies a PostgreSQL connection pool from a connection
// string owned by the application.
func NewPool(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL connection string: %w", err)
	}

	connectContext, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectContext, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL connection pool: %w", err)
	}
	if err := pool.Ping(connectContext); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return pool, nil
}
