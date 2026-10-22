package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/core/contract"
)

// PostgresTransaction binds only repository contracts into a use-case callback.
// The binder belongs in persistence/composition code; services never see pgx.Tx.
type PostgresTransaction[R any] struct {
	pool    *pgxpool.Pool
	bind    func(pgx.Tx) (R, error)
	options pgx.TxOptions
	cleanup time.Duration
}

func NewPostgresTransaction[R any](pool *pgxpool.Pool, bind func(pgx.Tx) (R, error), options pgx.TxOptions, cleanup time.Duration) (*PostgresTransaction[R], error) {
	if pool == nil || bind == nil || cleanup < time.Millisecond || cleanup > time.Minute {
		return nil, core.NewError(core.Invalid, "pool, repository binder, and bounded cleanup are required", nil)
	}
	return &PostgresTransaction[R]{pool: pool, bind: bind, options: options, cleanup: cleanup}, nil
}
func (u *PostgresTransaction[R]) Within(ctx context.Context, callback func(R) error) error {
	if callback == nil {
		return core.NewError(core.Invalid, "transaction callback is required", nil)
	}
	tx, err := u.pool.BeginTx(ctx, u.options)
	if err != nil {
		return core.NewError(core.Unavailable, "transaction unavailable", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), u.cleanup)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	repositories, err := u.bind(tx)
	if err != nil {
		return core.NewError(core.Unavailable, "bind transaction repositories failed", err)
	}
	if err = callback(repositories); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return core.NewError(core.Unavailable, "transaction commit failed", err)
	}
	return nil
}

var _ contract.Transaction[any] = (*PostgresTransaction[any])(nil)
