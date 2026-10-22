package database_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/database"
)

type recordRepository interface {
	Put(context.Context, int) error
}
type transactionalRecords struct {
	tx    pgx.Tx
	table string
}

func newTransactionalRecords(tx pgx.Tx, table string) *transactionalRecords {
	return &transactionalRecords{tx: tx, table: table}
}
func (r *transactionalRecords) Put(ctx context.Context, id int) error {
	_, err := r.tx.Exec(ctx, "INSERT INTO "+r.table+"(id) VALUES($1)", id)
	return err
}

func TestRepositoryTransactionBoundaries(t *testing.T) {
	address := os.Getenv("GOSTACK_TEST_DATABASE_URL")
	if address == "" {
		if os.Getenv("GOSTACK_REQUIRE_INTEGRATION") == "true" {
			t.Fatal("transaction integration database required")
		}
		t.Skip("set GOSTACK_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, address)
	if err != nil {
		t.Fatal("test connection failed")
	}
	defer pool.Close()
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := "gostack_tx_" + hex.EncodeToString(random[:])
	qualified := pgx.Identifier{schema, "records"}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("transaction fixture cleanup failed")
		}
	}()
	if _, err = pool.Exec(ctx, "CREATE TABLE "+qualified+"(id integer PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	u, err := database.NewPostgresTransaction[recordRepository](pool, func(tx pgx.Tx) (recordRepository, error) { return newTransactionalRecords(tx, qualified), nil }, pgx.TxOptions{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = u.Within(ctx, func(repository recordRepository) error { return repository.Put(ctx, 1) }); err != nil {
		t.Fatal(err)
	}
	rejected := core.NewError(core.Invalid, "business validation rejected", nil)
	if err = u.Within(ctx, func(repository recordRepository) error {
		if err := repository.Put(ctx, 2); err != nil {
			return err
		}
		return rejected
	}); !errors.Is(err, rejected) {
		t.Fatal("business error changed")
	}
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		_ = u.Within(ctx, func(repository recordRepository) error {
			if err := repository.Put(ctx, 3); err != nil {
				return err
			}
			panic("business panic")
		})
	}()
	if !panicked {
		t.Fatal("transaction swallowed panic")
	}
	canceled, stop := context.WithCancel(ctx)
	if err = u.Within(canceled, func(repository recordRepository) error {
		if err := repository.Put(canceled, 4); err != nil {
			return err
		}
		stop()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not roll back")
	}
	stop()
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+qualified).Scan(&count); err != nil || count != 1 {
		t.Fatalf("transaction atomicity failed: count=%d, error=%v", count, err)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("transaction leaked a connection")
	}
}
