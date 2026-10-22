package framework_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/framework"
	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/migration"
)

func TestRuntimeFactoryWithPostgres(t *testing.T) {
	address := os.Getenv("GOSTACK_TEST_RUNTIME_DATABASE_URL")
	if address == "" {
		if os.Getenv("GOSTACK_REQUIRE_INTEGRATION") == "true" {
			t.Fatal("runtime integration database is required")
		}
		t.Skip("set GOSTACK_TEST_RUNTIME_DATABASE_URL")
	}
	values := map[string]string{"GOSTACK_ENV": "test", "GOSTACK_DATABASE_URL": address, "GOSTACK_HTTP_ADDRESS": "127.0.0.1:0"}
	provider, _ := config.NewEnvProvider(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
	settings, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	source, err := migration.NewFileSource(os.DirFS("../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	logger, _ := logging.NewJSONLogger(io.Discard, "info")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app, err := framework.New(ctx, settings, logger, source)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = app.Run(ctx); err == nil {
		t.Fatal("closed application started")
	}
}
