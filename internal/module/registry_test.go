package module_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/tjahjono/gostack/httpkit/router"
	registry "github.com/tjahjono/gostack/internal/module"
	"github.com/tjahjono/gostack/moduleapi"
)

type delivery struct {
	routes        []router.Route
	ignoreInvalid bool
}

func (d *delivery) RegisterRoutes(r router.Registrar) error {
	for _, route := range d.routes {
		if err := r.Register(route); err != nil && !d.ignoreInvalid {
			return err
		}
	}
	return nil
}

type lifecycle struct {
	name             string
	trace            *[]string
	fail, panicStart bool
	stopError        bool
}

func (l *lifecycle) Start(context.Context) error {
	*l.trace = append(*l.trace, "start:"+l.name)
	if l.panicStart {
		panic("private initialization detail")
	}
	if l.fail {
		return errors.New("initialization failed")
	}
	return nil
}
func (l *lifecycle) Stop(context.Context) error {
	*l.trace = append(*l.trace, "stop:"+l.name)
	if l.stopError {
		return errors.New("cleanup failed")
	}
	return nil
}
func component(t *testing.T, name string, deps, permissions []string, routes []router.Route, life moduleapi.Lifecycle) moduleapi.Module {
	t.Helper()
	descriptor, err := moduleapi.NewDescriptor(name, deps, permissions)
	if err != nil {
		t.Fatal(err)
	}
	if life == nil {
		life = moduleapi.NewPassiveLifecycle()
	}
	m, err := moduleapi.New(descriptor, &delivery{routes: routes}, life)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func route(path string) router.Route {
	return router.Route{Method: "GET", Path: path, Policy: router.Policy{Access: router.Public}, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
}

func TestStableFrameworkFirstOrderAndSnapshots(t *testing.T) {
	audit := component(t, "audit", nil, nil, nil, nil)
	user := component(t, "user", []string{"audit"}, nil, nil, nil)
	report := component(t, "report", []string{"exports"}, nil, nil, nil)
	exports := component(t, "exports", []string{"user"}, nil, nil, nil)
	r, err := registry.NewRegistry([]moduleapi.Module{user, audit}, []moduleapi.Module{report, exports}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Names(), []string{"audit", "user", "exports", "report"}) {
		t.Fatal(r.Names())
	}
	names := r.Names()
	names[0] = "mutated"
	if r.Names()[0] != "audit" {
		t.Fatal("registry metadata mutated")
	}
}
func TestInvalidRegistrations(t *testing.T) {
	for _, test := range []struct {
		name             string
		builtins, custom []moduleapi.Module
	}{
		{"reserved", nil, []moduleapi.Module{component(t, "auth", nil, nil, nil, nil)}},
		{"duplicate", nil, []moduleapi.Module{component(t, "sample", nil, nil, nil, nil), component(t, "sample", nil, nil, nil, nil)}},
		{"missing", nil, []moduleapi.Module{component(t, "sample", []string{"missing"}, nil, nil, nil)}},
		{"cycle", nil, []moduleapi.Module{component(t, "first", []string{"second"}, nil, nil, nil), component(t, "second", []string{"first"}, nil, nil, nil)}},
		{"builtin depends on custom", []moduleapi.Module{component(t, "user", []string{"custom"}, nil, nil, nil)}, []moduleapi.Module{component(t, "custom", nil, nil, nil, nil)}},
		{"duplicate permission", nil, []moduleapi.Module{component(t, "first", nil, []string{"item:read"}, nil, nil), component(t, "second", nil, []string{"item:read"}, nil, nil)}},
		{"duplicate route", nil, []moduleapi.Module{component(t, "first", nil, nil, []router.Route{route("/api/v1/item")}, nil), component(t, "second", nil, nil, []router.Route{route("/api/v1/item")}, nil)}},
		{"outside version", nil, []moduleapi.Module{component(t, "sample", nil, nil, []router.Route{route("/unversioned")}, nil)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := registry.NewRegistry(test.builtins, test.custom, time.Second); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
	undeclared := route("/api/v1/item")
	undeclared.Policy = router.Policy{Access: router.Permission, Permission: "item:read"}
	if _, err := registry.NewRegistry(nil, []moduleapi.Module{component(t, "sample", nil, nil, []router.Route{undeclared}, nil)}, time.Second); err == nil {
		t.Fatal("undeclared permission accepted")
	}
}
func TestStartupFailureReversesPartialInitialization(t *testing.T) {
	for _, panics := range []bool{false, true} {
		trace := []string{}
		first := component(t, "first", nil, nil, nil, &lifecycle{name: "first", trace: &trace, stopError: true})
		second := component(t, "second", []string{"first"}, nil, nil, &lifecycle{name: "second", trace: &trace, fail: true, panicStart: panics})
		r, err := registry.NewRegistry(nil, []moduleapi.Module{second, first}, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.Start(context.Background()); err == nil {
			t.Fatal("failed start accepted")
		}
		if !reflect.DeepEqual(trace, []string{"start:first", "start:second", "stop:second", "stop:first"}) {
			t.Fatal(trace)
		}
		if err = r.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(trace) != 4 {
			t.Fatal("cleanup repeated")
		}
	}
}
func TestIgnoredRegistrationErrorsRemainFatal(t *testing.T) {
	descriptor, _ := moduleapi.NewDescriptor("sample", nil, nil)
	m, _ := moduleapi.New(descriptor, &delivery{routes: []router.Route{{}}, ignoreInvalid: true}, moduleapi.NewPassiveLifecycle())
	if _, err := registry.NewRegistry(nil, []moduleapi.Module{m}, time.Second); err == nil {
		t.Fatal("ignored collector error accepted")
	}
}
