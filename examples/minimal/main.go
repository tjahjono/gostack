// The minimal executable demonstrates explicit runtime and migration assembly.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/database"
	"github.com/tjahjono/gostack/framework"
	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/migration"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "healthcheck" {
		return healthcheck(args[1:])
	}
	provider, err := config.NewEnvProvider(os.LookupEnv)
	if err != nil {
		return err
	}
	settings, err := provider.Load()
	if err != nil {
		return err
	}
	logger, err := logging.NewJSONLogger(os.Stdout, settings.LogLevel)
	if err != nil {
		return err
	}
	source, err := migration.NewFileSource(os.DirFS(settings.MigrationDirectory))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 || (len(args) == 1 && args[0] == "serve") {
		app, err := framework.New(ctx, settings, logger, source)
		if err != nil {
			return err
		}
		return app.Run(ctx)
	}
	if len(args) >= 2 && args[0] == "migrate" {
		if settings.MigrationURL.Reveal() == "" {
			return errors.New("GOSTACK_MIGRATION_DATABASE_URL is required for migrations")
		}
		dbSettings := settings.Database
		dbSettings.URL = settings.MigrationURL
		dbSettings.QueryTimeout = settings.MigrationTimeout
		db, err := database.NewPostgres(ctx, dbSettings)
		if err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), settings.HTTP.ShutdownTimeout)
			defer cancel()
			_ = db.Close(cleanup)
		}()
		runner, err := migration.NewPostgresRunner(db.Pool(), source, settings.MigrationTimeout)
		if err != nil {
			return err
		}
		switch {
		case len(args) == 2 && args[1] == "up":
			err = runner.Up(ctx)
		case len(args) == 4 && args[1] == "down":
			steps, parseErr := strconv.Atoi(args[3])
			if parseErr != nil {
				return errors.New("rollback steps must be an integer")
			}
			err = runner.Down(ctx, args[2], steps)
		default:
			return errors.New("usage: migrate up | migrate down <app|framework> <steps>")
		}
		if err != nil {
			return err
		}
		logger.Log(ctx, slog.LevelInfo, "migrations completed")
		return nil
	}
	return errors.New("usage: serve | migrate up | migrate down <app|framework> <steps> | healthcheck [url]")
}

func healthcheck(args []string) error {
	if len(args) > 1 {
		return errors.New("healthcheck accepts one URL")
	}
	address := "http://127.0.0.1:8080/health/ready"
	if len(args) == 1 {
		address = args[0]
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(address)
	if err != nil {
		return errors.New("healthcheck connection failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("healthcheck is not ready")
	}
	return nil
}
