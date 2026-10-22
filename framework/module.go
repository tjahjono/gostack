package framework

import (
	"net/http"
	"time"

	"github.com/tjahjono/gostack/httpkit"
	"github.com/tjahjono/gostack/httpkit/router"
	internalmodule "github.com/tjahjono/gostack/internal/module"
	"github.com/tjahjono/gostack/moduleapi"
)

// Options accepts already constructed modules and typed HTTP-provider dependencies.
type Options struct {
	Modules        []moduleapi.Module
	HTTP           httpkit.Options
	StartupTimeout time.Duration
}
type Registry = internalmodule.Registry
type RegistryContract = internalmodule.RegistryContract

func NewRegistry(builtins, custom []moduleapi.Module, cleanup time.Duration) (*Registry, error) {
	return internalmodule.NewRegistry(builtins, custom, cleanup)
}

type healthDelivery struct{ handler http.Handler }

func newHealthDelivery(health *Health) *healthDelivery {
	mux := http.NewServeMux()
	health.RegisterRoutes(mux)
	return &healthDelivery{handler: mux}
}
func (d *healthDelivery) RegisterRoutes(registrar router.Registrar) error {
	for _, path := range []string{"/health/live", "/health/ready"} {
		if err := registrar.Register(router.Route{Method: "GET", Path: path, Policy: router.Policy{Access: router.Public}, Handler: d.handler}); err != nil {
			return err
		}
	}
	return nil
}
func newHealthModule(health *Health) (moduleapi.Module, error) {
	descriptor, err := moduleapi.NewDescriptor("health", nil, nil)
	if err != nil {
		return nil, err
	}
	return moduleapi.New(descriptor, newHealthDelivery(health), moduleapi.NewPassiveLifecycle())
}

var _ moduleapi.Delivery = (*healthDelivery)(nil)
