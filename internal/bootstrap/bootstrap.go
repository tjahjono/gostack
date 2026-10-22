// Package bootstrap assembles infrastructure without importing framework.
package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/database"
	"github.com/tjahjono/gostack/migration"
)

type Checker interface{ Check(context.Context) error }
type Assembler interface {
	Build(context.Context) (*Resources, error)
}
type Resources struct {
	Database  *database.Postgres
	Readiness Checker
}
type Builder struct {
	settings config.Config
	source   migration.Source
}

func New(settings config.Config, source migration.Source) (*Builder, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("migration source is required")
	}
	return &Builder{settings: settings, source: source}, nil
}

func (b *Builder) Build(ctx context.Context) (*Resources, error) {
	db, err := database.NewPostgres(ctx, b.settings.Database)
	if err != nil {
		return nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, b.settings.HTTP.HealthTimeout)
	defer cancel()
	if err = db.ValidateRuntimeRole(checkCtx); err != nil {
		closeDatabase(db)
		return nil, err
	}
	verifier, err := migration.NewPostgresRunner(db.Pool(), b.source, b.settings.MigrationTimeout)
	if err != nil {
		closeDatabase(db)
		return nil, err
	}
	readiness, err := NewReadiness(db, verifier)
	if err != nil {
		closeDatabase(db)
		return nil, err
	}
	if err = readiness.Check(checkCtx); err != nil {
		closeDatabase(db)
		return nil, err
	}
	return &Resources{Database: db, Readiness: readiness}, nil
}

func closeDatabase(db database.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = db.Close(ctx)
}

type Readiness struct {
	database   database.Client
	migrations migration.Verifier
}

func NewReadiness(db database.Client, migrations migration.Verifier) (*Readiness, error) {
	if db == nil || migrations == nil {
		return nil, errors.New("database and migration verifier are required")
	}
	return &Readiness{database: db, migrations: migrations}, nil
}
func (r *Readiness) Check(ctx context.Context) error {
	if err := r.database.Ping(ctx); err != nil {
		return err
	}
	return r.migrations.Verify(ctx)
}

var _ Assembler = (*Builder)(nil)
var _ Checker = (*Readiness)(nil)
