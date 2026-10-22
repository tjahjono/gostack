package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryKey int64 = 0x47535441434B01

type Runner interface {
	Up(context.Context) error
	Down(context.Context, string, int) error
}
type Verifier interface{ Verify(context.Context) error }
type PostgresRunner struct {
	pool    *pgxpool.Pool
	source  Source
	timeout time.Duration
}
type StateError struct{ Stream, Code string }

func (e *StateError) Error() string { return fmt.Sprintf("migration %s: %s", e.Stream, e.Code) }

func NewPostgresRunner(pool *pgxpool.Pool, source Source, timeout time.Duration) (*PostgresRunner, error) {
	if pool == nil || source == nil || timeout < time.Millisecond || timeout > 24*time.Hour {
		return nil, errors.New("pool, source, and bounded migration timeout are required")
	}
	return &PostgresRunner{pool: pool, source: source, timeout: timeout}, nil
}

func (r *PostgresRunner) Up(ctx context.Context) error { return r.run(ctx, "", 0) }
func (r *PostgresRunner) Down(ctx context.Context, stream string, steps int) error {
	if (stream != "app" && stream != "framework") || steps < 1 {
		return errors.New("rollback requires a valid stream and positive step count")
	}
	return r.run(ctx, stream, steps)
}

func versionTable(stream string) string {
	if stream == "framework" {
		return "public.gostack_schema_migrations"
	}
	return "public.app_schema_migrations"
}

func (r *PostgresRunner) run(parent context.Context, down string, steps int) error {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	manifest, err := r.source.Load()
	if err != nil {
		return err
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return errors.New("acquire migration connection failed")
	}
	defer conn.Release()
	if err = acquireLock(ctx, conn); err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		var unlocked bool
		if err := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock($1)", advisoryKey).Scan(&unlocked); err != nil || !unlocked {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	for _, stream := range []string{"framework", "app"} {
		_, err = conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+versionTable(stream)+" (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), version bigint NOT NULL CHECK(version >= -1), dirty boolean NOT NULL)")
		if err != nil {
			return &StateError{stream, "initialize version table failed"}
		}
		_, err = conn.Exec(ctx, "INSERT INTO "+versionTable(stream)+" (version,dirty) VALUES(-1,false) ON CONFLICT(singleton) DO NOTHING")
		if err != nil {
			return &StateError{stream, "initialize version failed"}
		}
	}
	if err = verify(ctx, conn, manifest, false); err != nil {
		return err
	}
	if down == "framework" {
		var v int
		if err = conn.QueryRow(ctx, "SELECT version FROM public.app_schema_migrations").Scan(&v); err != nil {
			return err
		}
		if v >= 0 {
			return errors.New("application migrations must be rolled back before framework migrations")
		}
	}
	for _, stream := range []string{"framework", "app"} {
		if down != "" && down != stream {
			continue
		}
		entries := manifest[stream]
		if down != "" {
			var v int
			if err = conn.QueryRow(ctx, "SELECT version FROM "+versionTable(stream)).Scan(&v); err != nil {
				return err
			}
			count := 0
			for _, e := range entries {
				if int(e.Version) <= v {
					count++
				}
			}
			if steps > count {
				return errors.New("rollback exceeds applied migration count")
			}
		}
		if len(entries) == 0 {
			continue
		}
		driver := newPostgresDriver(ctx, conn, stream, entries)
		engine, err := migrate.NewWithInstance("gostack", newMemorySource(entries), "postgres", driver)
		if err != nil {
			return err
		}
		engine.PrefetchMigrations = 1
		engine.LockTimeout = r.timeout
		if down == "" {
			err = engine.Up()
		} else {
			err = engine.Steps(-steps)
		}
		sourceErr, dbErr := engine.Close()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
		if sourceErr != nil || dbErr != nil {
			return &StateError{stream, "close migration engine failed"}
		}
	}
	return nil
}

func acquireLock(ctx context.Context, conn *pgxpool.Conn) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var acquired bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryKey).Scan(&acquired); err != nil {
			return errors.New("acquire migration deployment lock failed")
		}
		if acquired {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *PostgresRunner) Verify(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	manifest, err := r.source.Load()
	if err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("migration readiness connection failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err = verify(ctx, tx, manifest, true); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("migration readiness transaction failed")
	}
	return nil
}

func verify(ctx context.Context, q queryer, manifest map[string][]Entry, requireHead bool) error {
	var ledgerExists bool
	if err := q.QueryRow(ctx, "SELECT to_regclass('public.gostack_migration_checksums') IS NOT NULL").Scan(&ledgerExists); err != nil {
		return errors.New("check migration ledger failed")
	}
	for _, stream := range []string{"framework", "app"} {
		var version int
		var dirty bool
		if err := q.QueryRow(ctx, "SELECT version,dirty FROM "+versionTable(stream)+" WHERE singleton").Scan(&version, &dirty); err != nil {
			return &StateError{stream, "schema not initialized"}
		}
		if dirty {
			return &StateError{stream, "dirty state requires operator review"}
		}
		entries := manifest[stream]
		head := -1
		expected := make(map[int]string)
		for _, e := range entries {
			head = int(e.Version)
			if int(e.Version) <= version {
				expected[int(e.Version)] = e.Checksum
			}
		}
		if version > head || (version >= 0 && expected[version] == "") {
			return &StateError{stream, "unknown applied version"}
		}
		if requireHead && version != head {
			return &StateError{stream, "pending migrations"}
		}
		if !ledgerExists {
			if version >= 0 {
				return &StateError{stream, "checksum ledger unavailable"}
			}
			continue
		}
		rows, err := q.Query(ctx, "SELECT version,checksum FROM public.gostack_migration_checksums WHERE stream=$1", stream)
		if err != nil {
			return &StateError{stream, "checksum ledger unavailable"}
		}
		count := 0
		valid := true
		for rows.Next() {
			var v int
			var checksum string
			if err := rows.Scan(&v, &checksum); err != nil {
				valid = false
				break
			}
			if expected[v] != checksum {
				valid = false
			}
			count++
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil || !valid || count != len(expected) {
			return &StateError{stream, "checksum mismatch"}
		}
	}
	return nil
}

type postgresDriver struct {
	ctx     context.Context
	conn    *pgxpool.Conn
	stream  string
	entries []Entry
	current int
	pending *Entry
	up      bool
	mu      sync.Mutex
	locked  bool
}

func newPostgresDriver(ctx context.Context, conn *pgxpool.Conn, stream string, entries []Entry) *postgresDriver {
	return &postgresDriver{ctx: ctx, conn: conn, stream: stream, entries: entries, current: -1}
}
func (*postgresDriver) Open(string) (migratedb.Driver, error) {
	return nil, errors.New("use instance construction")
}
func (*postgresDriver) Close() error { return nil }
func (*postgresDriver) Drop() error  { return errors.New("database drop is not supported") }
func (d *postgresDriver) Lock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locked {
		return migratedb.ErrLocked
	}
	d.locked = true
	return nil
}
func (d *postgresDriver) Unlock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.locked {
		return migratedb.ErrNotLocked
	}
	d.locked = false
	return nil
}
func (d *postgresDriver) Version() (int, bool, error) {
	var v int
	var dirty bool
	err := d.conn.QueryRow(d.ctx, "SELECT version,dirty FROM "+versionTable(d.stream)+" WHERE singleton").Scan(&v, &dirty)
	if err != nil {
		return 0, false, &StateError{d.stream, "read version failed"}
	}
	d.current = v
	return v, dirty, nil
}
func (d *postgresDriver) SetVersion(v int, dirty bool) error {
	if dirty {
		d.up = v > d.current
		target := v
		if !d.up {
			target = d.current
		}
		d.pending = nil
		for i := range d.entries {
			if int(d.entries[i].Version) == target {
				d.pending = &d.entries[i]
				break
			}
		}
		if d.pending == nil {
			return &StateError{d.stream, "unknown target version"}
		}
	}
	_, err := d.conn.Exec(d.ctx, "UPDATE "+versionTable(d.stream)+" SET version=$1,dirty=$2 WHERE singleton", v, dirty)
	if err != nil {
		return &StateError{d.stream, "write version failed"}
	}
	if !dirty {
		d.current = v
	}
	return nil
}
func (d *postgresDriver) Run(reader io.Reader) error {
	if d.pending == nil {
		return &StateError{d.stream, "missing migration context"}
	}
	body, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return &StateError{d.stream, "read migration failed"}
	}
	tx, err := d.conn.Begin(d.ctx)
	if err != nil {
		return &StateError{d.stream, "begin migration failed"}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if !d.up {
		if _, err = tx.Exec(d.ctx, "DELETE FROM public.gostack_migration_checksums WHERE stream=$1 AND version=$2", d.stream, d.pending.Version); err != nil {
			return &StateError{d.stream, "remove checksum failed"}
		}
	}
	if _, err = tx.Exec(d.ctx, string(body), pgx.QueryExecModeSimpleProtocol); err != nil {
		return &StateError{d.stream, "SQL execution failed; inspect dirty state"}
	}
	if d.up {
		if _, err = tx.Exec(d.ctx, "INSERT INTO public.gostack_migration_checksums(stream,version,checksum) VALUES($1,$2,$3)", d.stream, d.pending.Version, d.pending.Checksum); err != nil {
			return &StateError{d.stream, "write checksum failed"}
		}
	}
	if err = tx.Commit(d.ctx); err != nil {
		return &StateError{d.stream, "commit failed; inspect dirty state"}
	}
	return nil
}

var _ Runner = (*PostgresRunner)(nil)
var _ Verifier = (*PostgresRunner)(nil)
var _ migratedb.Driver = (*postgresDriver)(nil)
