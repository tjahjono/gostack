// Package database manages explicitly owned PostgreSQL pools.
package database

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tjahjono/gostack/config"
)

type Client interface {
	Ping(context.Context) error
	Close(context.Context) error
}

type Postgres struct {
	pool   *pgxpool.Pool
	once   sync.Once
	closed chan struct{}
}

func NewPostgres(ctx context.Context, settings config.Database) (*Postgres, error) {
	if err := settings.Validate("development"); err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(settings.URL.Reveal())
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection settings")
	}
	pc.MaxConns = settings.MaxConnections
	pc.MinConns = 0
	pc.MaxConnLifetime = settings.MaxLifetime
	pc.MaxConnLifetimeJitter = settings.MaxLifetime / 10
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.ConnConfig.ConnectTimeout = settings.ConnectTimeout
	pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(settings.QueryTimeout.Milliseconds(), 10)
	pc.ConnConfig.RuntimeParams["lock_timeout"] = strconv.FormatInt(settings.LockTimeout.Milliseconds(), 10)
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(settings.QueryTimeout.Milliseconds(), 10)
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pc.ConnConfig.RuntimeParams["search_path"] = "public"
	connectCtx, cancel := context.WithTimeout(ctx, settings.ConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(connectCtx, pc)
	if err != nil {
		return nil, errors.New("create PostgreSQL pool failed")
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		if connectCtx.Err() != nil {
			return nil, connectCtx.Err()
		}
		return nil, errors.New("PostgreSQL connection failed")
	}
	return &Postgres{pool: pool, closed: make(chan struct{})}, nil
}

// Pool is supplied only to repository constructors at the composition root.
func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }
func (p *Postgres) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("PostgreSQL health check failed")
	}
	return nil
}

// ValidateRuntimeRole rejects elevated roles, schema creation, and table ownership.
func (p *Postgres) ValidateRuntimeRole(ctx context.Context) error {
	var unsafe bool
	err := p.pool.QueryRow(ctx, `SELECT r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls OR r.rolreplication
		OR EXISTS (SELECT 1 FROM pg_roles e WHERE (e.rolsuper OR e.rolcreatedb OR e.rolcreaterole OR e.rolbypassrls OR e.rolreplication) AND pg_has_role(current_user,e.oid,'MEMBER'))
		OR has_schema_privilege(current_user, 'public', 'CREATE')
		OR EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' AND c.relkind IN ('r','p') AND pg_has_role(current_user,c.relowner,'USAGE'))
		FROM pg_roles r WHERE r.rolname=current_user`).Scan(&unsafe)
	if err != nil {
		return errors.New("runtime database role verification failed")
	}
	if unsafe {
		return errors.New("runtime database role must not have administrative, schema creation, or table ownership privileges")
	}
	return nil
}

func (p *Postgres) Close(ctx context.Context) error {
	p.once.Do(func() { go func() { p.pool.Close(); close(p.closed) }() })
	select {
	case <-p.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ Client = (*Postgres)(nil)
