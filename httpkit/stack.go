package httpkit

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tjahjono/gostack/logging"
	"github.com/tjahjono/gostack/security"
)

type Options struct {
	ExternalOrigin string
	AllowedOrigins []string
	TrustedProxies []netip.Prefix
	MaxBodyBytes   int64
	RequestTimeout time.Duration
	Providers      security.Providers
}
type Middleware interface {
	Wrap(http.Handler) http.Handler
}
type Stack struct {
	options Options
	logger  logging.Logger
	entropy security.Entropy
	origins map[string]bool
}

func NewStack(options Options, logger logging.Logger, entropy security.Entropy) (*Stack, error) {
	if logger == nil || entropy == nil {
		return nil, errors.New("logger and request ID entropy are required")
	}
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = 1 << 20
	}
	if options.MaxBodyBytes < 1 || options.MaxBodyBytes > 64<<20 {
		return nil, errors.New("body limit must be between 1 byte and 64 MiB")
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 15 * time.Second
	}
	if options.RequestTimeout < time.Millisecond || options.RequestTimeout > 24*time.Hour {
		return nil, errors.New("request timeout must be between 1ms and 24h")
	}
	options.AllowedOrigins = append([]string(nil), options.AllowedOrigins...)
	options.TrustedProxies = append([]netip.Prefix(nil), options.TrustedProxies...)
	origins := make(map[string]bool)
	for _, origin := range options.AllowedOrigins {
		canonical, err := canonicalOrigin(origin)
		if err != nil {
			return nil, err
		}
		origins[canonical] = true
	}
	if options.ExternalOrigin != "" {
		canonical, err := canonicalOrigin(options.ExternalOrigin)
		if err != nil {
			return nil, err
		}
		options.ExternalOrigin = canonical
		origins[canonical] = true
	}
	for i, prefix := range options.TrustedProxies {
		if !prefix.IsValid() || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
			return nil, errors.New("trusted proxy prefixes must be valid explicit networks")
		}
		options.TrustedProxies[i] = prefix.Masked()
	}
	return &Stack{options: options, logger: logger, entropy: entropy, origins: origins}, nil
}
func (s *Stack) OriginList() []string {
	origins := make([]string, 0, len(s.origins))
	for origin := range s.origins {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	return origins
}
func (s *Stack) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.options.RequestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		var random [16]byte
		if _, err := io.ReadFull(s.entropy, random[:]); err != nil {
			WriteProblem(w, r, 503, "request_id_unavailable")
			return
		}
		id := hex.EncodeToString(random[:])
		w.Header().Set("X-Request-ID", id)
		r.Header.Set("X-Request-ID", id)
		r = r.WithContext(security.WithRequestID(r.Context(), id))
		tracked := newResponseWriter(w)
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(http.ErrAbortHandler)
				}
				s.logger.Log(r.Context(), slog.LevelError, "request panic recovered", slog.String("request_id", id))
				if tracked.wrote {
					panic(http.ErrAbortHandler)
				}
				WriteProblem(tracked, r, 500, "internal_error")
			}
		}()
		header := tracked.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		r, err := s.proxy(r)
		if err != nil {
			WriteProblem(tracked, r, 400, "invalid_forwarded_headers")
			return
		}
		if security.Scheme(r.Context()) == "https" {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if r.ContentLength > s.options.MaxBodyBytes {
			WriteProblem(tracked, r, 413, "payload_too_large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(tracked, r.Body, s.options.MaxBodyBytes)
		}
		if !s.cors(tracked, r) {
			return
		}
		next.ServeHTTP(tracked, r)
	})
}

func canonicalOrigin(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u == nil || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "#") || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", errors.New("origin must be an explicit HTTP(S) origin")
	}
	host := strings.ToLower(u.Hostname())
	if _, err := netip.ParseAddr(host); err != nil {
		for _, c := range host {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
				return "", errors.New("invalid origin hostname")
			}
		}
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid origin port")
		}
		port = strconv.Itoa(n)
	}
	if u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}

func (s *Stack) trusted(address netip.Addr) bool {
	for _, prefix := range s.options.TrustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
func (s *Stack) proxy(r *http.Request) (*http.Request, error) {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	address := peer.Addr().Unmap()
	if err != nil {
		address, err = netip.ParseAddr(r.RemoteAddr)
		if err != nil {
			return r, errors.New("invalid peer address")
		}
		address = address.Unmap()
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.trusted(address) {
		if len(r.Header.Values("Forwarded")) > 0 {
			return r, errors.New("use configured X-Forwarded headers only")
		}
		if values := r.Header.Values("X-Forwarded-Proto"); len(values) > 0 {
			if len(values) != 1 || (values[0] != "http" && values[0] != "https") {
				return r, errors.New("ambiguous forwarded scheme")
			}
			scheme = values[0]
		}
		if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
			if len(values) != 1 || len(values[0]) > 2048 {
				return r, errors.New("ambiguous forwarded chain")
			}
			parts := strings.Split(values[0], ",")
			if len(parts) > 32 {
				return r, errors.New("forwarded chain too long")
			}
			chain := make([]netip.Addr, len(parts))
			for i, part := range parts {
				parsed, err := netip.ParseAddr(strings.TrimSpace(part))
				if err != nil || parsed.Zone() != "" {
					return r, errors.New("invalid forwarded address")
				}
				chain[i] = parsed.Unmap()
			}
			for i := len(chain) - 1; i >= 0 && s.trusted(address); i-- {
				address = chain[i]
			}
		}
	}
	return r.WithContext(security.WithClientAddress(r.Context(), address, scheme)), nil
}

func (s *Stack) cors(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Add("Vary", "Origin")
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		WriteProblem(w, r, 403, "origin_denied")
		return false
	}
	origin, err := canonicalOrigin(values[0])
	if err != nil || !s.origins[origin] {
		WriteProblem(w, r, 403, "origin_denied")
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", values[0])
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	if r.Method != "OPTIONS" || r.Header.Get("Access-Control-Request-Method") == "" {
		return true
	}
	method, err := singleHeader(r, "Access-Control-Request-Method")
	if err != nil || !validCORSMethod(method) {
		WriteProblem(w, r, 403, "preflight_denied")
		return false
	}
	requested := r.Header.Values("Access-Control-Request-Headers")
	if len(requested) > 1 {
		WriteProblem(w, r, 403, "preflight_denied")
		return false
	}
	if len(requested) == 1 {
		for _, header := range strings.Split(requested[0], ",") {
			switch strings.ToLower(strings.TrimSpace(header)) {
			case "content-type", "x-csrf-token":
			default:
				WriteProblem(w, r, 403, "preflight_denied")
				return false
			}
		}
	}
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
	w.Header().Set("Access-Control-Allow-Methods", method)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
	return false
}
func validCORSMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return true
	}
	return false
}

// Unwrap preserves access to optional transport operations through ResponseController.
type responseWriter struct {
	http.ResponseWriter
	wrote bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}
func (w *responseWriter) WriteHeader(status int) {
	if status >= 200 {
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseWriter) Write(body []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(body)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) FlushError() error {
	w.wrote = true
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Flush() { _ = w.FlushError() }
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.wrote = true
	}
	return conn, buffer, err
}

var _ Middleware = (*Stack)(nil)
