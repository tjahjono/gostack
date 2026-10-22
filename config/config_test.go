package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/tjahjono/gostack/config"
)

func TestEnvironmentValidation(t *testing.T) {
	for _, test := range []struct {
		name, key, value string
		valid            bool
	}{
		{"defaults", "", "", true}, {"bad duration", "GOSTACK_HTTP_WRITE_TIMEOUT", "forever", false},
		{"zero timeout", "GOSTACK_DB_QUERY_TIMEOUT", "0s", false}, {"production TLS", "GOSTACK_ENV", "production", false},
		{"pool bound", "GOSTACK_DB_MAX_CONNECTIONS", "1001", false}, {"bad level", "GOSTACK_LOG_LEVEL", "verbose", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{"GOSTACK_ENV": "test", "GOSTACK_DATABASE_URL": "postgres://runtime:secret@localhost/gostack?sslmode=disable"}
			if test.key != "" {
				values[test.key] = test.value
			}
			p, err := config.NewEnvProvider(func(k string) (string, bool) { v, ok := values[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Load()
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
}

func TestSecureDefaultAndDuplicateTLSOptions(t *testing.T) {
	for _, address := range []string{"postgres://runtime:secret@localhost/gostack?sslmode=disable", "postgres://runtime:secret@localhost/gostack?sslmode=verify-full&sslmode=disable"} {
		provider, _ := config.NewEnvProvider(func(key string) (string, bool) {
			if key == "GOSTACK_DATABASE_URL" {
				return address, true
			}
			return "", false
		})
		if _, err := provider.Load(); err == nil {
			t.Fatal("unsafe default or ambiguous TLS accepted")
		}
	}
}

func TestSecretRedaction(t *testing.T) {
	s := config.NewSecret("never-print-this")
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{fmt.Sprint(s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), string(b)} {
		if strings.Contains(value, s.Reveal()) {
			t.Fatal("secret disclosed")
		}
	}
	p, _ := config.NewEnvProvider(func(string) (string, bool) { return "", false })
	if _, err := p.Load(); err == nil {
		t.Fatal("missing database URL accepted")
	}
	if _, err := config.NewEnvProvider(nil); err == nil {
		t.Fatal("nil lookup accepted")
	}
}
