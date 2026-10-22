package router_test

import (
	"net/http"
	"testing"

	"github.com/tjahjono/gostack/httpkit/router"
)

func TestPatternConflictsAreStartupErrors(t *testing.T) {
	route := func(path string) router.Route {
		return router.Route{Method: "GET", Path: path, Policy: router.Policy{Access: router.Public}, Handler: http.NotFoundHandler()}
	}
	for _, routes := range [][]router.Route{
		{route("/api/v1/items"), route("/api/v1/items")},
		{route("/api/v1/{resource}/latest"), route("/api/v1/posts/{id}")},
		{route("/api/v1/{invalid")},
		{route("/api/v1/../unversioned")},
	} {
		if err := router.ValidateSet(routes); err == nil {
			t.Fatal("ambiguous/invalid routes accepted")
		}
	}
}
