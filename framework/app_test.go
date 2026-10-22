package framework_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/framework"
	"github.com/tjahjono/gostack/logging"
)

type checker struct{ fail bool }

func (c *checker) Check(context.Context) error {
	if c.fail {
		return errors.New("sensitive database detail")
	}
	return nil
}

type database struct{ closed atomic.Bool }

func (*database) Ping(context.Context) error    { return nil }
func (d *database) Close(context.Context) error { d.closed.Store(true); return nil }

func TestHealthDoesNotDiscloseFailures(t *testing.T) {
	for _, test := range []struct {
		path        string
		ready, fail bool
		status      int
	}{
		{"/health/live", false, true, 200}, {"/health/ready", false, false, 503}, {"/health/ready", true, true, 503}, {"/health/ready", true, false, 200},
	} {
		h, err := framework.NewHealth(&checker{fail: test.fail}, time.Second, func() bool { return test.ready })
		if err != nil {
			t.Fatal(err)
		}
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s: %d", test.path, response.Code)
		}
		if response.Body.String() != "{\"status\":\"not_ready\"}\n" && test.status == 503 {
			t.Fatal("health leaked error")
		}
	}
}

func TestRunDrainsAndCloses(t *testing.T) {
	p, _ := config.NewEnvProvider(func(k string) (string, bool) {
		if k == "GOSTACK_ENV" {
			return "test", true
		}
		if k == "GOSTACK_DATABASE_URL" {
			return "postgres://runtime:secret@localhost/test?sslmode=disable", true
		}
		return "", false
	})
	cfg, err := p.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.Address = "127.0.0.1:0"
	db := &database{}
	log, _ := logging.NewJSONLogger(io.Discard, "info")
	app, err := framework.NewWithDependencies(cfg.HTTP, log, db, &checker{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- app.Run(ctx) }()
	cancel()
	select {
	case err := <-result:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked")
	}
	if !db.closed.Load() {
		t.Fatal("database was not closed")
	}
	if err := app.Run(context.Background()); err == nil {
		t.Fatal("second run accepted")
	}
}

type startLogger struct{ address chan string }

func (l *startLogger) Log(_ context.Context, _ slog.Level, message string, attrs ...slog.Attr) {
	if message == "application started" {
		for _, a := range attrs {
			if a.Key == "address" {
				l.address <- a.Value.String()
			}
		}
	}
}

type blockingChecker struct{ entered, release chan struct{} }

func (c *blockingChecker) Check(ctx context.Context) error {
	close(c.entered)
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestShutdownDrainsActiveRequest(t *testing.T) {
	provider, _ := config.NewEnvProvider(func(k string) (string, bool) {
		if k == "GOSTACK_ENV" {
			return "test", true
		}
		if k == "GOSTACK_DATABASE_URL" {
			return "postgres://runtime:secret@localhost/test?sslmode=disable", true
		}
		return "", false
	})
	cfg, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTP.Address = "127.0.0.1:0"
	log := &startLogger{address: make(chan string, 1)}
	db := &database{}
	health := &blockingChecker{entered: make(chan struct{}), release: make(chan struct{})}
	app, err := framework.NewWithDependencies(cfg.HTTP, log, db, health)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	go func() { completed <- app.Run(ctx) }()
	var address string
	select {
	case address = <-log.address:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start")
	}
	requestDone := make(chan error, 1)
	go func() {
		client := http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + address + "/health/ready")
		if err == nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-health.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach readiness")
	}
	cancel()
	if db.closed.Load() {
		t.Fatal("database closed before active request drained")
	}
	close(health.release)
	select {
	case err = <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	if err = <-requestDone; err != nil {
		t.Fatal(err)
	}
	if !db.closed.Load() {
		t.Fatal("database not closed")
	}
}
