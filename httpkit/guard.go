package httpkit

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/httpkit/router"
	"github.com/tjahjono/gostack/moduleapi"
	"github.com/tjahjono/gostack/security"
)

type PolicyGuard struct {
	providers                 security.Providers
	origins                   map[string]bool
	sessionCookie, csrfCookie string
}

func NewPolicyGuard(providers security.Providers, origins []string) (*PolicyGuard, error) {
	allowed := make(map[string]bool)
	for _, origin := range origins {
		canonical, err := canonicalOrigin(origin)
		if err != nil {
			return nil, err
		}
		allowed[canonical] = true
	}
	return &PolicyGuard{providers: providers, origins: allowed, sessionCookie: "__Host-gostack_session", csrfCookie: "__Host-gostack_csrf"}, nil
}
func (g *PolicyGuard) Wrap(route router.Route) (http.Handler, error) {
	if err := router.Validate(route); err != nil {
		return nil, err
	}
	policy := route.Policy
	unsafe := router.Unsafe(route.Method)
	if policy.Access != router.Public && g.providers.Authentication == nil {
		return nil, errors.New("authentication provider required")
	}
	if policy.Access == router.Permission && (!moduleapi.ValidPermission(policy.Permission) || g.providers.Authorization == nil) {
		return nil, errors.New("authorization provider and valid permission required")
	}
	if policy.RateBucket != "" && (!moduleapi.ValidName(policy.RateBucket) || g.providers.RateLimit == nil) {
		return nil, errors.New("rate limiter and valid bucket required")
	}
	if unsafe && (g.providers.CSRF == nil || len(g.origins) == 0 || policy.RateBucket == "") {
		return nil, errors.New("unsafe routes require CSRF, explicit origins, and rate limiting")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(security.WithPrincipal(r.Context(), security.Principal{}))
		if policy.RateBucket != "" {
			address, ok := security.ClientAddress(r.Context())
			if !ok {
				WriteProblem(w, r, 503, "rate_limit_unavailable")
				return
			}
			decision, err := g.providers.RateLimit.Allow(r.Context(), security.RateKey{Bucket: policy.RateBucket, Address: address})
			if err != nil || decision.RetryAfter < 0 || decision.RetryAfter > 24*time.Hour {
				WriteProblem(w, r, 503, "rate_limit_unavailable")
				return
			}
			if !decision.Allowed {
				seconds := int64((decision.RetryAfter + time.Second - 1) / time.Second)
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
				WriteProblem(w, r, 429, "rate_limited")
				return
			}
		}
		var principal security.Principal
		if policy.Access != router.Public {
			token, err := uniqueCookie(r, g.sessionCookie)
			if err != nil || token == "" {
				WriteProblem(w, r, 401, "authentication_required")
				return
			}
			principal, err = g.providers.Authentication.Authenticate(r.Context(), token)
			if err != nil {
				status := 503
				if core.IsCode(err, core.Unauthorized) || core.IsCode(err, core.Forbidden) {
					status = 401
				}
				WriteProblem(w, r, status, "authentication_failed")
				return
			}
			if !principal.Valid() {
				WriteProblem(w, r, 503, "authentication_unavailable")
				return
			}
			r = r.WithContext(security.WithPrincipal(r.Context(), principal))
		}
		if unsafe {
			origin, err := singleHeader(r, "Origin")
			if err != nil {
				WriteProblem(w, r, 403, "csrf_failed")
				return
			}
			canonical, err := canonicalOrigin(origin)
			if err != nil || !g.origins[canonical] {
				WriteProblem(w, r, 403, "csrf_failed")
				return
			}
			token, err := singleHeader(r, "X-CSRF-Token")
			if err != nil || len(token) < 16 || len(token) > 512 {
				WriteProblem(w, r, 403, "csrf_failed")
				return
			}
			binding, err := uniqueCookie(r, g.csrfCookie)
			if err != nil {
				WriteProblem(w, r, 403, "csrf_failed")
				return
			}
			err = g.providers.CSRF.Verify(r.Context(), security.CSRFProof{Principal: principal, Token: token, Binding: binding})
			if err != nil {
				status := 503
				if core.IsCode(err, core.Forbidden) || core.IsCode(err, core.Unauthorized) {
					status = 403
				}
				WriteProblem(w, r, status, "csrf_failed")
				return
			}
		}
		if policy.Access == router.Permission {
			if err := g.providers.Authorization.Authorize(r.Context(), principal, policy.Permission); err != nil {
				status := 503
				if core.IsCode(err, core.Forbidden) || core.IsCode(err, core.Unauthorized) {
					status = 403
				}
				WriteProblem(w, r, status, "access_denied")
				return
			}
		}
		route.Handler.ServeHTTP(w, r)
	}), nil
}
func uniqueCookie(r *http.Request, name string) (string, error) {
	value := ""
	found := false
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			if found || len(cookie.Value) > 512 {
				return "", errors.New("ambiguous cookie")
			}
			found = true
			value = cookie.Value
		}
	}
	return value, nil
}
func singleHeader(r *http.Request, name string) (string, error) {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return "", errors.New("missing or ambiguous header")
	}
	return values[0], nil
}

var _ router.Guard = (*PolicyGuard)(nil)
