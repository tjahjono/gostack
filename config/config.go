// Package config loads validated, explicitly supplied application settings.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"
)

// Secret redacts itself from formatting, structured logging, and JSON.
type Secret struct{ value string }

func NewSecret(value string) Secret         { return Secret{value: value} }
func (s Secret) Reveal() string             { return s.value }
func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) LogValue() slog.Value         { return slog.StringValue("[REDACTED]") }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

type Database struct {
	URL                                                    Secret
	MaxConnections                                         int32
	ConnectTimeout, QueryTimeout, LockTimeout, MaxLifetime time.Duration
}

type HTTP struct {
	Address                                                   string
	ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout time.Duration
	ShutdownTimeout, HealthTimeout                            time.Duration
	MaxHeaderBytes                                            int
}

type Config struct {
	Environment, LogLevel, MigrationDirectory string
	Database                                  Database
	MigrationURL                              Secret
	MigrationTimeout                          time.Duration
	HTTP                                      HTTP
}

type Provider interface{ Load() (Config, error) }
type EnvProvider struct{ lookup func(string) (string, bool) }

func NewEnvProvider(lookup func(string) (string, bool)) (*EnvProvider, error) {
	if lookup == nil {
		return nil, errors.New("environment lookup is required")
	}
	return &EnvProvider{lookup: lookup}, nil
}

func (p *EnvProvider) Load() (Config, error) {
	c := Config{Environment: "production", LogLevel: "info", MigrationDirectory: "migrations", MigrationTimeout: 2 * time.Minute,
		Database: Database{MaxConnections: 10, ConnectTimeout: 5 * time.Second, QueryTimeout: 5 * time.Second, LockTimeout: time.Second, MaxLifetime: 30 * time.Minute},
		HTTP:     HTTP{Address: "127.0.0.1:8080", ReadTimeout: 15 * time.Second, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, ShutdownTimeout: 15 * time.Second, HealthTimeout: 2 * time.Second, MaxHeaderBytes: 16384}}
	stringsByKey := map[string]*string{"GOSTACK_ENV": &c.Environment, "GOSTACK_LOG_LEVEL": &c.LogLevel, "GOSTACK_HTTP_ADDRESS": &c.HTTP.Address, "GOSTACK_MIGRATION_DIRECTORY": &c.MigrationDirectory}
	for key, target := range stringsByKey {
		if value, ok := p.lookup(key); ok {
			*target = value
		}
	}
	if value, ok := p.lookup("GOSTACK_DATABASE_URL"); ok {
		c.Database.URL = NewSecret(value)
	}
	if value, ok := p.lookup("GOSTACK_MIGRATION_DATABASE_URL"); ok {
		c.MigrationURL = NewSecret(value)
	}
	durations := map[string]*time.Duration{
		"GOSTACK_DB_CONNECT_TIMEOUT": &c.Database.ConnectTimeout, "GOSTACK_DB_QUERY_TIMEOUT": &c.Database.QueryTimeout,
		"GOSTACK_DB_LOCK_TIMEOUT": &c.Database.LockTimeout, "GOSTACK_DB_MAX_LIFETIME": &c.Database.MaxLifetime,
		"GOSTACK_HTTP_READ_TIMEOUT": &c.HTTP.ReadTimeout, "GOSTACK_HTTP_READ_HEADER_TIMEOUT": &c.HTTP.ReadHeaderTimeout,
		"GOSTACK_HTTP_WRITE_TIMEOUT": &c.HTTP.WriteTimeout, "GOSTACK_HTTP_IDLE_TIMEOUT": &c.HTTP.IdleTimeout,
		"GOSTACK_HTTP_SHUTDOWN_TIMEOUT": &c.HTTP.ShutdownTimeout, "GOSTACK_HTTP_HEALTH_TIMEOUT": &c.HTTP.HealthTimeout,
		"GOSTACK_MIGRATION_TIMEOUT": &c.MigrationTimeout,
	}
	for key, target := range durations {
		if value, ok := p.lookup(key); ok {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration", key)
			}
			*target = parsed
		}
	}
	if value, ok := p.lookup("GOSTACK_DB_MAX_CONNECTIONS"); ok {
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return Config{}, errors.New("GOSTACK_DB_MAX_CONNECTIONS must be an integer")
		}
		c.Database.MaxConnections = int32(n)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) Validate() error {
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return errors.New("GOSTACK_ENV must be development, test, or production")
	}
	if err := c.HTTP.Validate(); err != nil {
		return err
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return errors.New("invalid log level")
	}
	if c.MigrationDirectory == "" {
		return errors.New("migration directory is required")
	}
	if err := c.Database.Validate(c.Environment); err != nil {
		return err
	}
	if c.MigrationURL.Reveal() != "" {
		db := c.Database
		db.URL = c.MigrationURL
		if err := db.Validate(c.Environment); err != nil {
			return errors.New("invalid migration database configuration")
		}
	}
	if c.MigrationTimeout < time.Millisecond || c.MigrationTimeout > 24*time.Hour {
		return errors.New("migration timeout must be between 1ms and 24h")
	}
	return nil
}

func (h HTTP) Validate() error {
	if _, err := net.ResolveTCPAddr("tcp", h.Address); err != nil || h.Address == "" {
		return errors.New("invalid HTTP listen address")
	}
	for _, d := range []time.Duration{h.ReadTimeout, h.ReadHeaderTimeout, h.WriteTimeout, h.IdleTimeout, h.ShutdownTimeout, h.HealthTimeout} {
		if d < time.Millisecond || d > 24*time.Hour {
			return errors.New("HTTP timeouts must be between 1ms and 24h")
		}
	}
	if h.HealthTimeout > time.Minute || h.HealthTimeout >= h.WriteTimeout || h.MaxHeaderBytes < 1024 || h.MaxHeaderBytes > 1<<20 {
		return errors.New("invalid HTTP timeout or header budget")
	}
	return nil
}

func (d Database) Validate(environment string) error {
	u, err := url.Parse(d.URL.Reveal())
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" || u.User == nil || u.User.Username() == "" {
		return errors.New("database URL must be an explicit PostgreSQL URL")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid database URL query")
	}
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("duplicate database URL options are prohibited")
		}
		switch key {
		case "sslmode", "sslrootcert", "sslcert", "sslkey", "application_name":
		default:
			return errors.New("unsupported database URL option")
		}
	}
	mode := query.Get("sslmode")
	if mode != "disable" && mode != "verify-full" {
		return errors.New("database sslmode must be explicit: disable or verify-full")
	}
	if environment == "production" && mode != "verify-full" {
		return errors.New("production database TLS must use verify-full")
	}
	if d.MaxConnections < 1 || d.MaxConnections > 1000 {
		return errors.New("database connection limit must be between 1 and 1000")
	}
	for _, v := range []time.Duration{d.ConnectTimeout, d.QueryTimeout, d.LockTimeout, d.MaxLifetime} {
		if v < time.Millisecond || v > 24*time.Hour {
			return errors.New("database timeouts must be between 1ms and 24h")
		}
	}
	if d.LockTimeout > d.QueryTimeout {
		return errors.New("database lock timeout exceeds query timeout")
	}
	return nil
}

var _ Provider = (*EnvProvider)(nil)
