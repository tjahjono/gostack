// Package router declares routes without importing services or persistence.
package router

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Access uint8

const (
	Public Access = iota + 1
	Authenticated
	Permission
)

type Policy struct {
	Access                 Access
	Permission, RateBucket string
}
type Route struct {
	Method, Path string
	Policy       Policy
	Handler      http.Handler
}
type Registrar interface{ Register(Route) error }
type Guard interface {
	Wrap(Route) (http.Handler, error)
}

func Validate(route Route) error {
	if route.Handler == nil || !strings.HasPrefix(route.Path, "/") || strings.ContainsAny(route.Path, " \t\r\n?#") {
		return errors.New("route requires a handler and absolute path pattern")
	}
	for _, segment := range strings.Split(route.Path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("route patterns cannot contain traversal segments")
		}
	}
	if strings.Contains(route.Path, "//") {
		return errors.New("route patterns must use canonical paths")
	}
	switch route.Method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		return errors.New("unsupported route method")
	}
	if route.Policy.Access < Public || route.Policy.Access > Permission {
		return errors.New("route access policy must be explicit")
	}
	if (route.Policy.Access == Permission) != (route.Policy.Permission != "") {
		return errors.New("permission policy requires exactly one permission key")
	}
	return nil
}
func Unsafe(method string) bool { return method != "GET" && method != "HEAD" && method != "OPTIONS" }

type Collector struct {
	routes []Route
	err    error
}

func NewCollector() *Collector { return &Collector{} }
func (c *Collector) Register(route Route) error {
	if err := Validate(route); err != nil {
		c.err = errors.Join(c.err, err)
		return err
	}
	c.routes = append(c.routes, route)
	return nil
}
func (c *Collector) Err() error      { return c.err }
func (c *Collector) Routes() []Route { return append([]Route(nil), c.routes...) }

type Mux struct{ mux *http.ServeMux }

func ValidateSet(routes []Route) error {
	mux := http.NewServeMux()
	for _, route := range routes {
		if err := Validate(route); err != nil {
			return err
		}
		if err := handle(mux, route.Method+" "+route.Path, route.Handler); err != nil {
			return err
		}
	}
	return nil
}
func NewMux(routes []Route, guard Guard) (*Mux, error) {
	if guard == nil {
		return nil, errors.New("route guard is required")
	}
	if err := ValidateSet(routes); err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	for _, route := range routes {
		if err := Validate(route); err != nil {
			return nil, err
		}
		handler, err := guard.Wrap(route)
		if err != nil {
			return nil, fmt.Errorf("route %s %s: %w", route.Method, route.Path, err)
		}
		if handler == nil {
			return nil, errors.New("route guard returned a nil handler")
		}
		if err := handle(mux, route.Method+" "+route.Path, handler); err != nil {
			return nil, err
		}
	}
	return &Mux{mux: mux}, nil
}
func handle(mux *http.ServeMux, pattern string, handler http.Handler) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("invalid or conflicting route pattern: %s", pattern)
		}
	}()
	mux.Handle(pattern, handler)
	return nil
}
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.mux.ServeHTTP(w, r) }

var _ Registrar = (*Collector)(nil)
var _ http.Handler = (*Mux)(nil)
