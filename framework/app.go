// Package framework provides the reusable GoStack application runtime.
package framework

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/database"
	"github.com/tjahjono/gostack/internal/bootstrap"
	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/migration"
)

type Application interface {
	Run(context.Context) error
	Close(context.Context) error
}
type App struct {
	server   *http.Server
	logger   logging.Logger
	database database.Client
	settings config.HTTP
	ready    atomic.Bool
	mu       sync.Mutex
	started  bool
	closed   bool
}

// New validates infrastructure and schema before creating a server.
func New(ctx context.Context, settings config.Config, logger logging.Logger, source migration.Source) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	builder, err := bootstrap.New(settings, source)
	if err != nil {
		return nil, err
	}
	resources, err := builder.Build(ctx)
	if err != nil {
		return nil, err
	}
	app, err := NewWithDependencies(settings.HTTP, logger, resources.Database, resources.Readiness)
	if err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), settings.HTTP.ShutdownTimeout)
		defer cancel()
		_ = resources.Database.Close(cleanup)
		return nil, err
	}
	return app, nil
}

// NewWithDependencies allows explicit infrastructure substitution for testing.
func NewWithDependencies(settings config.HTTP, logger logging.Logger, db database.Client, checker Readiness) (*App, error) {
	if logger == nil || db == nil {
		return nil, errors.New("logger and database lifecycle are required")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	a := &App{logger: logger, database: db, settings: settings}
	health, err := NewHealth(checker, settings.HealthTimeout, a.ready.Load)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	health.RegisterRoutes(mux)
	writer, err := logging.NewWriter(logger)
	if err != nil {
		return nil, err
	}
	a.server = &http.Server{Addr: settings.Address, Handler: mux, ReadTimeout: settings.ReadTimeout, ReadHeaderTimeout: settings.ReadHeaderTimeout, WriteTimeout: settings.WriteTimeout, IdleTimeout: settings.IdleTimeout, MaxHeaderBytes: settings.MaxHeaderBytes, ErrorLog: log.New(writer, "", 0)}
	return a, nil
}

func (a *App) Run(ctx context.Context) (result error) {
	a.mu.Lock()
	if a.started || a.closed {
		a.mu.Unlock()
		return errors.New("application can only run once and must not be closed")
	}
	a.started = true
	a.mu.Unlock()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), a.settings.ShutdownTimeout)
		defer cancel()
		result = errors.Join(result, a.Close(cleanup))
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", a.settings.Address)
	if err != nil {
		return errors.New("HTTP listener failed")
	}
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	a.server.BaseContext = func(net.Listener) context.Context { return requestCtx }
	completed := make(chan error, 1)
	a.ready.Store(true)
	go func() { completed <- a.server.Serve(listener) }()
	a.logger.Log(ctx, slog.LevelInfo, "application started", slog.String("address", listener.Addr().String()))
	select {
	case err = <-completed:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP server failed")
		}
		return nil
	case <-ctx.Done():
		a.ready.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.settings.ShutdownTimeout)
		defer cancel()
		if err = a.server.Shutdown(shutdownCtx); err != nil {
			_ = a.server.Close()
			<-completed
			return errors.New("HTTP drain exceeded shutdown deadline")
		}
		if err = <-completed; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP shutdown failed")
		}
		a.logger.Log(context.Background(), slog.LevelInfo, "application stopped")
		return nil
	}
}

func (a *App) Close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.ready.Store(false)
	serverErr := a.server.Shutdown(ctx)
	if serverErr != nil {
		_ = a.server.Close()
	}
	return errors.Join(serverErr, a.database.Close(ctx))
}

var _ Application = (*App)(nil)
