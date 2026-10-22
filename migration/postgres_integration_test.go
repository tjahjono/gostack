package migration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tjahjono/gostack/migration"
)

func isolatedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	address := os.Getenv("GOSTACK_TEST_DATABASE_URL")
	if address == "" {
		if os.Getenv("GOSTACK_REQUIRE_INTEGRATION") == "true" {
			t.Fatal("integration database is required")
		}
		t.Skip("set GOSTACK_TEST_DATABASE_URL for real PostgreSQL tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, address)
	if err != nil {
		t.Fatal("connect test administrator failed")
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	name := "gostack_test_" + hex.EncodeToString(random[:])
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatal("create isolated database failed")
	}
	pc, err := pgxpool.ParseConfig(address)
	if err != nil {
		t.Fatal("parse test connection failed")
	}
	pc.ConnConfig.Database = name
	pc.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		defer admin.Close(cleanup)
		if !strings.HasPrefix(name, "gostack_test_") || len(name) != 37 {
			t.Error("unsafe test database cleanup refused")
			return
		}
		if _, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("isolated database cleanup failed")
		}
	})
	return pool
}

func fixture(t *testing.T) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{"app/20261022000001_sample.up.sql": {Data: []byte("CREATE TABLE public.sample(id bigint PRIMARY KEY);")}, "app/20261022000001_sample.down.sql": {Data: []byte("DROP TABLE public.sample;")}}
	for _, direction := range []string{"up", "down"} {
		path := "framework/20261022000000_migration_checksums." + direction + ".sql"
		data, err := fs.ReadFile(os.DirFS("../migrations"), path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = &fstest.MapFile{Data: data}
	}
	return result
}

func runner(t *testing.T, pool *pgxpool.Pool, files fstest.MapFS, timeout time.Duration) *migration.PostgresRunner {
	t.Helper()
	source, err := migration.NewFileSource(files)
	if err != nil {
		t.Fatal(err)
	}
	r, err := migration.NewPostgresRunner(pool, source, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPostgresMigrationLifecycle(t *testing.T) {
	pool := isolatedDatabase(t)
	files := fixture(t)
	r := runner(t, pool, files, 10*time.Second)
	ctx := context.Background()
	if err := r.Verify(ctx); err == nil {
		t.Fatal("uninitialized schema passed readiness")
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- r.Up(ctx) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	path := "app/20261022000001_sample.down.sql"
	original := files[path].Data
	files[path].Data = []byte("DROP TABLE public.sample; -- changed")
	if err := r.Up(ctx); err == nil {
		t.Fatal("mutated applied migration accepted")
	}
	files[path].Data = original
	if err := r.Down(ctx, "framework", 1); err == nil {
		t.Fatal("dependency-breaking rollback accepted")
	}
	if err := r.Down(ctx, "app", 2); err == nil {
		t.Fatal("excessive rollback accepted")
	}
	if err := r.Down(ctx, "app", 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx); err == nil {
		t.Fatal("pending app migration passed readiness")
	}
	if err := r.Down(ctx, "framework", 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresFailedSQLIsAtomicAndDirty(t *testing.T) {
	pool := isolatedDatabase(t)
	files := fixture(t)
	files["app/20261022000001_sample.up.sql"].Data = []byte("CREATE TABLE public.sample(id bigint); SELECT * FROM public.nonexistent_table;")
	r := runner(t, pool, files, 5*time.Second)
	ctx := context.Background()
	if err := r.Up(ctx); err == nil {
		t.Fatal("invalid SQL accepted")
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.sample') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatal("failed DDL did not roll back")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.gostack_migration_checksums WHERE stream='app'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration retained checksum")
	}
	var state *migration.StateError
	if err := r.Up(ctx); !errors.As(err, &state) || !strings.Contains(state.Code, "dirty") {
		t.Fatalf("dirty state was not preserved: %v", err)
	}
}

func TestPostgresDeploymentLockHasDeadline(t *testing.T) {
	pool := isolatedDatabase(t)
	ctx := context.Background()
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = held.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(0x47535441434B01)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = held.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(0x47535441434B01))
		held.Release()
	})
	r := runner(t, pool, fixture(t), 150*time.Millisecond)
	start := time.Now()
	if err := r.Up(ctx); err == nil {
		t.Fatal("concurrent deployment lock was bypassed")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("lock wait exceeded budget")
	}
}
