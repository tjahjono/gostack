package httpkit_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tjahjono/gostack/httpkit"
	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/security"
)

func TestJSONBoundary(t *testing.T) {
	for _, test := range []struct {
		name, body, contentType string
		limit                   int64
		chunked                 bool
		status                  int
	}{
		{"valid", `{"name":"item"}`, "application/json", 100, false, 204},
		{"unknown field", `{"name":"item","admin":true}`, "application/json", 100, false, 400},
		{"trailing value", `{"name":"item"}{}`, "application/json", 100, false, 400},
		{"content type", `{"name":"item"}`, "text/plain", 100, false, 415},
		{"known length", `{"name":"item"}`, "application/json", 4, false, 413},
		{"chunked length", `{"name":"item"}`, "application/json", 4, true, 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger, _ := logging.NewJSONLogger(io.Discard, "info")
			stack, _ := httpkit.NewStack(httpkit.Options{MaxBodyBytes: test.limit}, logger, security.NewSecureRandom())
			handler := stack.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := httpkit.DecodeJSON[struct {
					Name string `json:"name"`
				}](r)
				if err != nil {
					httpkit.WriteError(w, r, err)
					return
				}
				w.WriteHeader(204)
			}))
			r := httptest.NewRequest("POST", "http://app.test/", strings.NewReader(test.body))
			r.Header.Set("Content-Type", test.contentType)
			if test.chunked {
				r.ContentLength = -1
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != test.status {
				t.Fatalf("%d: %s", response.Code, response.Body)
			}
		})
	}
}
