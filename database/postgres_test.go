package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/database"
)

func TestCanceledConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := database.NewPostgres(ctx, config.Database{URL: config.NewSecret("postgres://runtime:secret@127.0.0.1:1/test?sslmode=disable"), MaxConnections: 1, ConnectTimeout: time.Second, QueryTimeout: time.Second, LockTimeout: time.Second, MaxLifetime: time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
