// Package module orders explicitly constructed modules and owns their lifecycle.
package module

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tjahjono/gostack/httpkit/router"
	"github.com/tjahjono/gostack/moduleapi"
)

type RegistryContract interface {
	Names() []string
	Routes() []router.Route
	Start(context.Context) error
	Stop(context.Context) error
}
type entry struct {
	module     moduleapi.Module
	descriptor moduleapi.Descriptor
	builtin    bool
}
type Registry struct {
	entries []entry
	routes  []router.Route
	gate    chan struct{}
	started int
	state   uint8
	cleanup time.Duration
}

func NewRegistry(builtins, custom []moduleapi.Module, cleanup time.Duration) (*Registry, error) {
	if cleanup < time.Millisecond || cleanup > 24*time.Hour {
		return nil, errors.New("module cleanup timeout must be between 1ms and 24h")
	}
	all := make(map[string]entry)
	input := append(append([]moduleapi.Module(nil), builtins...), custom...)
	inputNames := make([]string, 0, len(input))
	for i, m := range input {
		if m == nil {
			return nil, errors.New("nil module")
		}
		descriptor := m.Descriptor()
		name := descriptor.Name()
		if !moduleapi.ValidName(name) {
			return nil, errors.New("invalid module descriptor")
		}
		if _, exists := all[name]; exists {
			return nil, fmt.Errorf("duplicate module: %s", name)
		}
		builtin := i < len(builtins)
		if !builtin && reserved(name) {
			return nil, fmt.Errorf("reserved framework module: %s", name)
		}
		all[name] = entry{module: m, descriptor: descriptor, builtin: builtin}
		inputNames = append(inputNames, name)
	}
	for _, e := range all {
		for _, dependency := range e.descriptor.Dependencies() {
			target, exists := all[dependency]
			if !exists {
				return nil, fmt.Errorf("module %s requires missing module %s", e.descriptor.Name(), dependency)
			}
			if e.builtin && !target.builtin {
				return nil, errors.New("framework modules cannot depend on custom modules")
			}
		}
	}
	states := make(map[string]uint8)
	ordered := make([]entry, 0, len(input))
	var visit func(string) error
	visit = func(name string) error {
		if states[name] == 1 {
			return fmt.Errorf("module dependency cycle at %s", name)
		}
		if states[name] == 2 {
			return nil
		}
		states[name] = 1
		e := all[name]
		for _, dependency := range e.descriptor.Dependencies() {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		states[name] = 2
		ordered = append(ordered, e)
		return nil
	}
	for _, name := range inputNames {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	permissions := make(map[string]bool)
	for _, e := range ordered {
		for _, key := range e.descriptor.Permissions() {
			if permissions[key] {
				return nil, fmt.Errorf("duplicate permission: %s", key)
			}
			permissions[key] = true
		}
	}
	routes := make([]router.Route, 0)
	for _, e := range ordered {
		collector := router.NewCollector()
		if err := e.module.RegisterRoutes(collector); err != nil {
			return nil, fmt.Errorf("register %s: %w", e.descriptor.Name(), err)
		}
		if err := collector.Err(); err != nil {
			return nil, err
		}
		for _, route := range collector.Routes() {
			if e.descriptor.Name() != "health" && route.Path != "/api/v1" && !strings.HasPrefix(route.Path, "/api/v1/") {
				return nil, errors.New("module routes must use /api/v1")
			}
			if route.Policy.Access == router.Permission && !permissions[route.Policy.Permission] {
				return nil, fmt.Errorf("undeclared permission: %s", route.Policy.Permission)
			}
			routes = append(routes, route)
		}
	}
	if err := router.ValidateSet(routes); err != nil {
		return nil, err
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &Registry{entries: ordered, routes: routes, gate: gate, cleanup: cleanup}, nil
}
func reserved(name string) bool {
	switch name {
	case "health", "user", "users", "role", "roles", "permission", "permissions", "auth", "session", "sessions", "password", "passwords", "mfa", "ldap", "saml", "identity", "identities", "audit", "audit_logs":
		return true
	}
	return false
}
func (r *Registry) Names() []string {
	names := make([]string, len(r.entries))
	for i, e := range r.entries {
		names[i] = e.descriptor.Name()
	}
	return names
}
func (r *Registry) Routes() []router.Route { return append([]router.Route(nil), r.routes...) }
func (r *Registry) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-r.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Registry) Start(ctx context.Context) error {
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer func() { r.gate <- struct{}{} }()
	if r.state != 0 {
		return errors.New("module registry can only start once")
	}
	r.state = 1
	for i, e := range r.entries {
		r.started = i + 1
		if err := invoke(ctx, e.module.Start); err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.cleanup)
			defer cancel()
			return errors.Join(fmt.Errorf("start %s: %w", e.descriptor.Name(), err), r.stop(cleanup))
		}
	}
	return nil
}
func (r *Registry) Stop(ctx context.Context) error {
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer func() { r.gate <- struct{}{} }()
	return r.stop(ctx)
}
func (r *Registry) stop(ctx context.Context) error {
	if r.state == 2 {
		return nil
	}
	r.state = 2
	var result error
	for i := r.started - 1; i >= 0; i-- {
		if err := invoke(ctx, r.entries[i].module.Stop); err != nil {
			result = errors.Join(result, fmt.Errorf("stop %s: %w", r.entries[i].descriptor.Name(), err))
		}
	}
	r.started = 0
	return result
}
func invoke(ctx context.Context, callback func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("module lifecycle panicked")
		}
	}()
	return callback(ctx)
}

var _ RegistryContract = (*Registry)(nil)
