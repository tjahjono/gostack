package framework_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/tjahjono/gostack/config"
	"github.com/tjahjono/gostack/framework"
	"github.com/tjahjono/gostack/httpkit/router"
	"github.com/tjahjono/gostack/moduleapi"
)

type moduleDelivery struct{}

func (*moduleDelivery) RegisterRoutes(r router.Registrar) error {
	return r.Register(router.Route{Method: "GET", Path: "/api/v1/sample", Policy: router.Policy{Access: router.Public}, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })})
}

type moduleLifecycle struct {
	events   chan string
	database *database
	fail     bool
}

func (l *moduleLifecycle) Start(context.Context) error {
	l.events <- "start"
	if l.fail {
		return errors.New("module unavailable")
	}
	return nil
}
func (l *moduleLifecycle) Stop(context.Context) error {
	if l.database.closed.Load() {
		return errors.New("database closed before module stop")
	}
	l.events <- "stop"
	return nil
}

func TestApplicationModuleLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		provider, _ := config.NewEnvProvider(func(key string) (string, bool) {
			switch key {
			case "GOSTACK_ENV":
				return "test", true
			case "GOSTACK_DATABASE_URL":
				return "postgres://runtime@localhost/test?sslmode=disable", true
			}
			return "", false
		})
		settings, err := provider.Load()
		if err != nil {
			t.Fatal(err)
		}
		settings.HTTP.Address = "127.0.0.1:0"
		db := &database{}
		events := make(chan string, 4)
		life := &moduleLifecycle{events: events, database: db, fail: fail}
		descriptor, _ := moduleapi.NewDescriptor("sample", []string{"health"}, nil)
		component, err := moduleapi.New(descriptor, &moduleDelivery{}, life)
		if err != nil {
			t.Fatal(err)
		}
		logger := &startLogger{address: make(chan string, 1)}
		app, err := framework.NewWithDependencies(settings.HTTP, logger, db, &checker{}, framework.Options{Modules: []moduleapi.Module{component}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		completed := make(chan error, 1)
		go func() { completed <- app.Run(ctx) }()
		if !fail {
			select {
			case address := <-logger.address:
				client := http.Client{Timeout: time.Second}
				response, err := client.Get("http://" + address + "/api/v1/sample")
				if err != nil {
					cancel()
					t.Fatal(err)
				}
				_ = response.Body.Close()
				if response.StatusCode != 204 {
					cancel()
					t.Fatal(response.StatusCode)
				}
			case <-time.After(3 * time.Second):
				cancel()
				t.Fatal("custom module never served")
			}
		}
		if !fail {
			cancel()
		}
		select {
		case err = <-completed:
			if (err != nil) != fail {
				t.Fatalf("failure=%v: %v", fail, err)
			}
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("module lifecycle blocked")
		}
		cancel()
		if !db.closed.Load() {
			t.Fatal("database not closed")
		}
		if len(events) != 2 || <-events != "start" || <-events != "stop" {
			t.Fatal("module did not initialize/clean up exactly once")
		}
	}
}
