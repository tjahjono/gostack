package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/database"
)

func TestRuntimeRoleAndBoundedClose(t *testing.T) {
	address := os.Getenv("GOSTACK_TEST_RUNTIME_DATABASE_URL")
	if address == "" {
		if os.Getenv("GOSTACK_REQUIRE_INTEGRATION") == "true" {
			t.Fatal("runtime test database is required")
		}
		t.Skip("set GOSTACK_TEST_RUNTIME_DATABASE_URL")
	}
	settings := config.Database{URL: config.NewSecret(address), MaxConnections: 2, ConnectTimeout: 3 * time.Second, QueryTimeout: time.Second, LockTimeout: time.Second, MaxLifetime: time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := database.NewPostgres(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	if err = db.ValidateRuntimeRole(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "CREATE TABLE public.forbidden(id int)"); err == nil {
		t.Fatal("runtime role can create tables")
	}
	if _, err = db.Pool().Exec(ctx, "UPDATE public.gostack_migration_checksums SET checksum=checksum"); err == nil {
		t.Fatal("runtime role can mutate migration integrity records")
	}
	held, err := db.Pool().Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err = db.Close(short); err == nil {
		held.Release()
		t.Fatal("close ignored outstanding acquisition")
	}
	held.Release()
	if err = db.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
