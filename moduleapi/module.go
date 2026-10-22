// Package moduleapi contains leaf module contracts, avoiding module import cycles.
package moduleapi

import (
	"context"
	"errors"
	"strings"

	"github.com/tjahjono/gostack/httpkit/router"
)

type Delivery interface{ RegisterRoutes(router.Registrar) error }

// Start's context bounds initialization; workers own their cancellation until Stop.
// Start/Stop must honor their deadlines; Stop also cleans up a partially failed Start.
type Lifecycle interface {
	Start(context.Context) error
	Stop(context.Context) error
}
type Module interface {
	Delivery
	Lifecycle
	Descriptor() Descriptor
}
type Descriptor struct {
	name                      string
	dependencies, permissions []string
}

func NewDescriptor(name string, dependencies, permissions []string) (Descriptor, error) {
	if !ValidName(name) {
		return Descriptor{}, errors.New("invalid module name")
	}
	seen := make(map[string]bool)
	for _, dependency := range dependencies {
		if !ValidName(dependency) || dependency == name || seen[dependency] {
			return Descriptor{}, errors.New("invalid or duplicate module dependency")
		}
		seen[dependency] = true
	}
	seen = make(map[string]bool)
	for _, permission := range permissions {
		if !ValidPermission(permission) || seen[permission] {
			return Descriptor{}, errors.New("invalid or duplicate permission")
		}
		seen[permission] = true
	}
	return Descriptor{name: name, dependencies: append([]string(nil), dependencies...), permissions: append([]string(nil), permissions...)}, nil
}
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func ValidPermission(key string) bool {
	parts := strings.Split(key, ":")
	return len(parts) == 2 && ValidName(parts[0]) && ValidName(parts[1])
}
func (d Descriptor) Name() string           { return d.name }
func (d Descriptor) Dependencies() []string { return append([]string(nil), d.dependencies...) }
func (d Descriptor) Permissions() []string  { return append([]string(nil), d.permissions...) }

type Component struct {
	descriptor Descriptor
	delivery   Delivery
	lifecycle  Lifecycle
}

func New(descriptor Descriptor, delivery Delivery, lifecycle Lifecycle) (*Component, error) {
	if !ValidName(descriptor.Name()) || delivery == nil || lifecycle == nil {
		return nil, errors.New("descriptor, delivery, and lifecycle are required")
	}
	return &Component{descriptor: descriptor, delivery: delivery, lifecycle: lifecycle}, nil
}
func (c *Component) Descriptor() Descriptor                  { return c.descriptor }
func (c *Component) RegisterRoutes(r router.Registrar) error { return c.delivery.RegisterRoutes(r) }
func (c *Component) Start(ctx context.Context) error         { return c.lifecycle.Start(ctx) }
func (c *Component) Stop(ctx context.Context) error          { return c.lifecycle.Stop(ctx) }

// PassiveLifecycle is for modules that own no background resources.
type PassiveLifecycle struct{}

func NewPassiveLifecycle() *PassiveLifecycle              { return &PassiveLifecycle{} }
func (*PassiveLifecycle) Start(ctx context.Context) error { return ctx.Err() }
func (*PassiveLifecycle) Stop(ctx context.Context) error  { return ctx.Err() }

var _ Module = (*Component)(nil)
var _ Lifecycle = (*PassiveLifecycle)(nil)
