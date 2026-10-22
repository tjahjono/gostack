package httpkit_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/httpkit"
	"github.com/tjahjono/gostack/httpkit/router"
	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/security"
)

type providers struct {
	trace                                    []string
	authErr, permissionErr, csrfErr, rateErr error
	blocked, invalidPrincipal                bool
}

func (p *providers) Authenticate(_ context.Context, token string) (security.Principal, error) {
	p.trace = append(p.trace, "authentication")
	if p.authErr != nil {
		return security.Principal{}, p.authErr
	}
	if token != "session-token" {
		return security.Principal{}, core.NewError(core.Unauthorized, "private authentication reason", nil)
	}
	if p.invalidPrincipal {
		return security.Principal{}, nil
	}
	return security.NewPrincipal("user-1", "session-1", security.Primary)
}
func (p *providers) Authorize(context.Context, security.Principal, string) error {
	p.trace = append(p.trace, "authorization")
	return p.permissionErr
}
func (p *providers) Verify(_ context.Context, proof security.CSRFProof) error {
	p.trace = append(p.trace, "csrf")
	if p.csrfErr != nil {
		return p.csrfErr
	}
	if proof.Token != "0123456789abcdef" || proof.Binding != "bound" {
		return core.NewError(core.Forbidden, "private CSRF reason", nil)
	}
	return nil
}
func (p *providers) Allow(context.Context, security.RateKey) (security.Decision, error) {
	p.trace = append(p.trace, "rate")
	return security.Decision{Allowed: !p.blocked, RetryAfter: 1500 * time.Millisecond}, p.rateErr
}
func dependencies(p *providers) security.Providers {
	return security.Providers{Authentication: p, Authorization: p, CSRF: p, RateLimit: p}
}
func protected(t *testing.T, p *providers) http.Handler {
	t.Helper()
	logger, _ := logging.NewJSONLogger(io.Discard, "info")
	stack, err := httpkit.NewStack(httpkit.Options{ExternalOrigin: "https://app.test"}, logger, security.NewSecureRandom())
	if err != nil {
		t.Fatal(err)
	}
	guard, err := httpkit.NewPolicyGuard(dependencies(p), stack.OriginList())
	if err != nil {
		t.Fatal(err)
	}
	route := router.Route{Method: "POST", Path: "/api/v1/items", Policy: router.Policy{Access: router.Permission, Permission: "items:create", RateBucket: "api"}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := security.PrincipalFrom(r.Context()); !ok || principal.SubjectID() != "user-1" {
			t.Error("verified principal missing")
		}
		p.trace = append(p.trace, "handler")
		w.WriteHeader(204)
	})}
	mux, err := router.NewMux([]router.Route{route}, guard)
	if err != nil {
		t.Fatal(err)
	}
	return stack.Wrap(mux)
}
func request() *http.Request {
	r := httptest.NewRequest("POST", "https://app.test/api/v1/items", nil)
	r.Header.Set("Origin", "https://app.test")
	r.Header.Set("X-CSRF-Token", "0123456789abcdef")
	r.AddCookie(&http.Cookie{Name: "__Host-gostack_session", Value: "session-token"})
	r.AddCookie(&http.Cookie{Name: "__Host-gostack_csrf", Value: "bound"})
	return r
}

func TestPolicyOrderAndFailureBoundaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*providers, *http.Request)
		status  int
	}{
		{"allowed", func(*providers, *http.Request) {}, 204},
		{"missing cookie", func(_ *providers, r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"ambiguous cookie", func(_ *providers, r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "__Host-gostack_session", Value: "second"})
		}, 401},
		{"auth storage failure", func(p *providers, _ *http.Request) { p.authErr = errors.New("secret database detail") }, 503},
		{"invalid provider principal", func(p *providers, _ *http.Request) { p.invalidPrincipal = true }, 503},
		{"bad csrf", func(_ *providers, r *http.Request) { r.Header.Set("X-CSRF-Token", "wrong") }, 403},
		{"cross origin", func(_ *providers, r *http.Request) { r.Header.Set("Origin", "https://attacker.test") }, 403},
		{"permission denied", func(p *providers, _ *http.Request) {
			p.permissionErr = core.NewError(core.Forbidden, "private rule", nil)
		}, 403},
		{"permission storage failure", func(p *providers, _ *http.Request) { p.permissionErr = errors.New("secret database detail") }, 503},
		{"rate storage failure", func(p *providers, _ *http.Request) { p.rateErr = errors.New("secret database detail") }, 503},
		{"throttled", func(p *providers, _ *http.Request) { p.blocked = true }, 429},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := &providers{}
			r := request()
			test.prepare(p, r)
			handler := protected(t, p)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != test.status {
				t.Fatalf("got %d: %s", response.Code, response.Body)
			}
			if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "private") {
				t.Fatal("private error disclosed")
			}
			if test.status == 204 && !reflect.DeepEqual(p.trace, []string{"rate", "authentication", "csrf", "authorization", "handler"}) {
				t.Fatal(p.trace)
			}
			if test.status != 204 {
				for _, step := range p.trace {
					if step == "handler" {
						t.Fatal("denied request reached handler")
					}
				}
			}
			if test.status == 429 && response.Header().Get("Retry-After") != "2" {
				t.Fatal("invalid retry budget")
			}
		})
	}
}

func TestMissingProvidersRejectRoutesAtConstruction(t *testing.T) {
	guard, _ := httpkit.NewPolicyGuard(security.Providers{}, nil)
	for _, policy := range []router.Policy{{}, {Access: router.Authenticated}, {Access: router.Permission, Permission: "items:read"}, {Access: router.Public, RateBucket: "api"}} {
		_, err := guard.Wrap(router.Route{Method: "GET", Path: "/api/v1/items", Policy: policy, Handler: http.NotFoundHandler()})
		if err == nil {
			t.Fatal("incomplete policy accepted")
		}
	}
	if _, err := guard.Wrap(router.Route{Method: "POST", Path: "/api/v1/items", Policy: router.Policy{Access: router.Public}, Handler: http.NotFoundHandler()}); err == nil {
		t.Fatal("unsafe public route accepted without CSRF and limiter")
	}
}

func TestTrustedProxyTraversal(t *testing.T) {
	for _, test := range []struct {
		name, remote, chain, scheme, want string
		status                            int
	}{
		{"trusted chain", "10.0.0.2:1234", "198.51.100.8, 203.0.113.7, 10.0.0.1", "https", "203.0.113.7", 200},
		{"untrusted peer", "203.0.113.9:1234", "198.51.100.8", "https", "203.0.113.9", 200},
		{"malformed chain", "10.0.0.2:1234", "invalid", "https", "", 400},
		{"ambiguous scheme", "10.0.0.2:1234", "198.51.100.8", "https,http", "", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := logging.NewJSONLogger(io.Discard, "info")
			stack, err := httpkit.NewStack(httpkit.Options{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}, logger, security.NewSecureRandom())
			if err != nil {
				t.Fatal(err)
			}
			handler := stack.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ip, _ := security.ClientAddress(r.Context())
				_, _ = io.WriteString(w, ip.String())
			}))
			r := httptest.NewRequest("GET", "http://app.test/", nil)
			r.RemoteAddr = test.remote
			r.Header.Set("X-Forwarded-For", test.chain)
			r.Header.Set("X-Forwarded-Proto", test.scheme)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != test.status {
				t.Fatalf("%d: %s", response.Code, response.Body)
			}
			if test.status == 200 && response.Body.String() != test.want {
				t.Fatal(response.Body.String())
			}
			if test.name == "trusted chain" && response.Header().Get("Strict-Transport-Security") == "" {
				t.Fatal("trusted HTTPS scheme lost")
			}
			if test.name == "untrusted peer" && response.Header().Get("Strict-Transport-Security") != "" {
				t.Fatal("untrusted scheme accepted")
			}
		})
	}
}

func TestRecoveryAndRequestIdentity(t *testing.T) {
	var logs bytes.Buffer
	logger, _ := logging.NewJSONLogger(&logs, "info")
	stack, _ := httpkit.NewStack(httpkit.Options{}, logger, security.NewSecureRandom())
	r := httptest.NewRequest("GET", "https://app.test/", nil)
	r.Header.Set("X-Request-ID", "forged")
	response := httptest.NewRecorder()
	stack.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if security.RequestID(r.Context()) == "forged" || r.Header.Get("X-Request-ID") == "forged" {
			t.Fatal("client request ID trusted")
		}
		panic("secret panic payload")
	})).ServeHTTP(response, r)
	if response.Code != 500 || len(response.Header().Get("X-Request-ID")) != 32 || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing secure failure response")
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("panic detail disclosed")
	}
}

func TestCORSDeniesUnknownOriginsAndAllowsCredentialedPreflight(t *testing.T) {
	logger, _ := logging.NewJSONLogger(io.Discard, "info")
	stack, _ := httpkit.NewStack(httpkit.Options{AllowedOrigins: []string{"https://frontend.test"}}, logger, security.NewSecureRandom())
	handler := stack.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("preflight reached business handler") }))
	for _, origin := range []string{"https://frontend.test", "https://attacker.test"} {
		r := httptest.NewRequest("OPTIONS", "https://api.test/api/v1/items", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "Content-Type, X-CSRF-Token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		expected := 204
		if origin != "https://frontend.test" {
			expected = 403
		}
		if response.Code != expected {
			t.Fatal(response.Code)
		}
		if expected == 204 && (response.Header().Get("Access-Control-Allow-Origin") != origin || response.Header().Get("Access-Control-Allow-Credentials") != "true") {
			t.Fatal("missing exact credentialed origin")
		}
	}
}

func TestRequestDeadlineAndResponseController(t *testing.T) {
	logger, _ := logging.NewJSONLogger(io.Discard, "info")
	stack, _ := httpkit.NewStack(httpkit.Options{RequestTimeout: 5 * time.Millisecond}, logger, security.NewSecureRandom())
	response := httptest.NewRecorder()
	stack.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
		}
	})).ServeHTTP(response, httptest.NewRequest("GET", "http://app.test/", nil))
	if !response.Flushed {
		t.Fatal("response controller could not reach underlying writer")
	}
}
