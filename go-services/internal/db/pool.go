package db

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool builds a pgx connection pool with the same guards as the Node pool:
// bounded size and connect/statement/idle-in-transaction timeouts. Used by the
// web service (the poller/exporter use a single connection each).
func NewPool(ctx context.Context, connectTimeout time.Duration, statementTimeoutMs, idleTxTimeoutMs, maxConns int) (*pgxpool.Pool, error) {
	url, err := URL()
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	if statementTimeoutMs > 0 {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(statementTimeoutMs)
	}
	if idleTxTimeoutMs > 0 {
		cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.Itoa(idleTxTimeoutMs)
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
