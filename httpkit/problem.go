// Package httpkit provides HTTP boundaries and fail-closed security composition.
package httpkit

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/security"
)

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: "urn:gostack:error:" + code, Title: http.StatusText(status), Status: status, Code: code, RequestID: security.RequestID(r.Context())})
}

type RequestError struct {
	status int
	code   string
	cause  error
}

func NewRequestError(status int, code string, cause error) *RequestError {
	if status < 400 || status > 599 {
		status = 500
		code = "internal_error"
	}
	return &RequestError{status: status, code: code, cause: cause}
}
func (e *RequestError) Error() string { return http.StatusText(e.status) }
func (e *RequestError) Unwrap() error { return e.cause }
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var request *RequestError
	if errors.As(err, &request) {
		WriteProblem(w, r, request.status, request.code)
		return
	}
	status, code := 500, "internal_error"
	for _, mapping := range []struct {
		code   core.Code
		status int
	}{{core.Invalid, 422}, {core.NotFound, 404}, {core.Conflict, 409}, {core.Unauthorized, 401}, {core.Forbidden, 403}, {core.Unavailable, 503}} {
		if core.IsCode(err, mapping.code) {
			status = mapping.status
			code = string(mapping.code)
			break
		}
	}
	WriteProblem(w, r, status, code)
}

// DecodeJSON rejects unknown fields, trailing values, and oversized bodies.
// Stack must wrap the handler to enforce its body limit for chunked requests.
func DecodeJSON[T any](r *http.Request) (T, error) {
	var value T
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return value, NewRequestError(415, "unsupported_media_type", nil)
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&value); err != nil {
		return value, decodeError(err)
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, decodeError(err)
	}
	return value, nil
}
func decodeError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return NewRequestError(413, "payload_too_large", err)
	}
	return NewRequestError(400, "invalid_json", err)
}
