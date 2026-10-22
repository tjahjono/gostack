package framework

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Readiness interface{ Check(context.Context) error }
type HealthHandler interface{ RegisterRoutes(*http.ServeMux) }
type Health struct {
	checker Readiness
	timeout time.Duration
	ready   func() bool
}

func NewHealth(checker Readiness, timeout time.Duration, ready func() bool) (*Health, error) {
	if checker == nil || ready == nil || timeout < time.Millisecond || timeout > time.Minute {
		return nil, errors.New("readiness checker, state, and bounded timeout are required")
	}
	return &Health{checker: checker, timeout: timeout, ready: ready}, nil
}

func (h *Health) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { writeHealth(w, http.StatusOK, "alive") })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if !h.ready() {
			writeHealth(w, http.StatusServiceUnavailable, "not_ready")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()
		if err := h.checker.Check(ctx); err != nil || !h.ready() {
			writeHealth(w, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeHealth(w, http.StatusOK, "ready")
	})
}

func writeHealth(w http.ResponseWriter, status int, state string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"status":"` + state + `"}` + "\n"))
}

var _ HealthHandler = (*Health)(nil)
