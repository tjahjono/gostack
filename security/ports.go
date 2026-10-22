// Package security defines request identity and typed security-provider ports.
package security

import (
	"context"
	"crypto/rand"
	"errors"
	"net/netip"
	"time"
)

type Assurance uint8

const (
	Primary Assurance = iota + 1
	MultiFactor
)

type Principal struct {
	subject, session string
	assurance        Assurance
}

func NewPrincipal(subject, session string, assurance Assurance) (Principal, error) {
	if !identifier(subject) || !identifier(session) || (assurance != Primary && assurance != MultiFactor) {
		return Principal{}, errors.New("invalid principal")
	}
	return Principal{subject: subject, session: session, assurance: assurance}, nil
}
func identifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func (p Principal) SubjectID() string    { return p.subject }
func (p Principal) SessionID() string    { return p.session }
func (p Principal) Assurance() Assurance { return p.assurance }
func (p Principal) Valid() bool {
	return identifier(p.subject) && identifier(p.session) && (p.assurance == Primary || p.assurance == MultiFactor)
}

type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}
type Authorizer interface {
	Authorize(context.Context, Principal, string) error
}
type CSRFProof struct {
	Principal      Principal
	Token, Binding string
}
type CSRFVerifier interface {
	Verify(context.Context, CSRFProof) error
}
type RateKey struct {
	Bucket  string
	Address netip.Addr
}
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}
type RateLimiter interface {
	Allow(context.Context, RateKey) (Decision, error)
}
type Providers struct {
	Authentication Authenticator
	Authorization  Authorizer
	CSRF           CSRFVerifier
	RateLimit      RateLimiter
}

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func NewSystemClock() *SystemClock  { return &SystemClock{} }
func (*SystemClock) Now() time.Time { return time.Now().UTC() }

type Entropy interface{ Read([]byte) (int, error) }
type SecureRandom struct{}

func NewSecureRandom() *SecureRandom             { return &SecureRandom{} }
func (*SecureRandom) Read(p []byte) (int, error) { return rand.Read(p) }

type contextKey uint8

const (
	principalKey contextKey = iota
	requestIDKey
	addressKey
	schemeKey
)

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok && p.Valid()
}
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey).(string); return id }
func WithClientAddress(ctx context.Context, address netip.Addr, scheme string) context.Context {
	return context.WithValue(context.WithValue(ctx, addressKey, address), schemeKey, scheme)
}
func ClientAddress(ctx context.Context) (netip.Addr, bool) {
	a, ok := ctx.Value(addressKey).(netip.Addr)
	return a, ok && a.IsValid()
}
func Scheme(ctx context.Context) string { s, _ := ctx.Value(schemeKey).(string); return s }

var _ Clock = (*SystemClock)(nil)
var _ Entropy = (*SecureRandom)(nil)
