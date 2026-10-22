# Security design

## Boundaries

Treat HTTP input, proxies, browser state, directory data, assertions, notifications, logs, and the database as separate trust boundaries. Database disclosure must not reveal usable session/reset/recovery credentials or plaintext TOTP secrets. Runtime/key compromise requires incident response and rotation.

TLS terminates at a configured ingress or application. Only explicit trusted proxy CIDRs may supply forwarded identity/scheme; inspect chains from the trusted side. Configure the external origin/base URL instead of trusting arbitrary Host headers.

## Passwords and authentication

Use Argon2id, a fresh salt of at least 16 bytes, a 32-byte result, and versioned PHC encoding. Initial target: 64 MiB, three iterations, one lane, subject to load benchmarking and bounded hashing concurrency. Reject malformed encodings and excessive verification parameters before allocation. Rehash successful logins when policy changes.

Default policy: 15-128 Unicode code points, maximum 1024 UTF-8 bytes, no mandatory character classes, common/breached-password blocklist, last five passwords prohibited. Verify each history entry with its recorded parameters. No silent truncation. Periodic changes are organizational policy, not an unconditional default; compromise triggers reset. Administrative reset requires change on next login.

Use generic failures and dummy hashing for unknown users. Default lockout: five failures in a 15-minute observation window cause a 15-minute lock; account state and shared limits prevent replica bypass. An administrative lock is separate and indefinite. Deactivation/administrative lock revokes sessions. Switching providers never bypasses account state.

User creation, login, history, and bootstrap share hashing ports. `user:create-admin` uses a hidden prompt or protected stdin, never password flags/defaults/logs. It creates the first administrator atomically with audit; later privileged grants require an authorized administrative workflow.

## Sessions and HTTP

Use 32 random bytes from `crypto/rand` per session token; store SHA-256 digests only and compare secret verifiers in constant time where applicable. Cookie: `__Host-gostack_session`, `Secure`, `HttpOnly`, `Path=/`, no Domain, `SameSite=Lax`. Default idle lifetime: 15 minutes; absolute lifetime: eight hours. Rotate on authentication/assurance changes. Rotation cannot revive revoked state.

Unsafe cookie-authenticated requests require a session-bound random CSRF token and validated Origin. Login uses pre-authentication CSRF and origin checks. Credential/reset responses use `Cache-Control: no-store`. CORS denies by default; credentialed access requires exact configured origins. GET never changes authentication state.

SAML ACS replaces the ordinary CSRF-header check with signed-response validation and stored one-time browser/request correlation. A separate short-lived `SameSite=None; Secure; HttpOnly` correlation cookie is scoped to ACS; it cannot use the `__Host-` prefix because of its restricted path. Consume on success or terminal failure. The main Lax cookie need not accompany the cross-site POST. IdP-initiated login is initially disabled.

Middleware order: request ID/recovery, trusted proxies, secure headers and size/time limits, route-specific CORS/protocol policy, rate limiting, session authentication, applicable CSRF, permission checks, handler. Sensitive mutations recheck authority transactionally. Use CSP, frame restrictions, nosniff, referrer policy, and HSTS for validated HTTPS deployments. Production rejects insecure-cookie mode. Unavailable authentication/limiter storage fails closed.

## MFA

Use a tested RFC-compatible TOTP adapter: 30-second steps, six digits, at most one-step tolerance. Encrypt secrets with AEAD, fresh nonces, key IDs, and associated user/method identity. Keys come from deployment secret management, not the database or repository. Enrollment expires in ten minutes and activates only after verification; return its provisioning URI once.

Challenges expire within five minutes, allow five attempts, and are single-use. Atomically reject reused/older accepted counters across challenges. Generate ten recovery codes with at least 128 random bits each; digest, display once, consume atomically. Rotation replaces the batch. Disable/rotation requires recent primary authentication plus an existing factor or audited administrative recovery. Administrative recovery revokes sessions and notifies the owner.

## LDAP and Active Directory

Use `go-ldap/ldap/v3` behind directory contracts. Production requires LDAPS or StartTLS before binding; reject plaintext binds and certificate-verification bypass. Validate server/trust roots, bound deadlines/page sizes, escape filters/DNs, and reject empty passwords.

Search through a minimally privileged service account, require one immutable subject, and authenticate on a separate user-bound connection. Never reuse that connection for service operations. Store/log no directory passwords. Map stable object identifiers, not mutable email.

Sync is paged/idempotent and preserves provider provenance. Outages or partial scans never trigger mass deactivation. Only completed authoritative scans or explicit disabled signals remove access. Group mapping is allowlisted; external membership cannot grant `super_admin` by default. Nested traversal is explicit, bounded, and cycle-safe.

## SAML

Use a maintained verification library behind a port. `crewjam/saml` is the initial candidate; implementation reviews its version and defaults. Trust only configured validated metadata/certificates. Restrict metadata destinations, redirects, time, and size to prevent unintended network access.

Reject unsigned assertions and SHA-1 signatures. Validate signed-element binding, issuer, destination, recipient, audience, validity, subject confirmation, `InResponseTo`, and at most 60 seconds skew. Business attributes must come from the verified signed assertion. Reject ambiguous/wrapped assertions, disable external entity resolution, and bound XML/decompression.

SP-initiated requests expire after five minutes and bind to the browser. RelayState is opaque and references an allowlisted local path. JIT keys on provider/immutable subject, never auto-links by email. Consume request/replay state transactionally with provisioning and session/challenge creation. SAML satisfies local MFA only through an explicit tested assurance policy. Support bounded overlapping certificate rotation.

## Audit and operations

Record login/logout/failure, password changes/resets, grants/permission changes, MFA enrollment/disable, LDAP login/sync, SAML login, session revocation, identity linking, and administrative actions. Security mutations and audit commit together; append failure prevents success. Record rejected operations in a separate durable failure transaction. Operational logs are fallback signals, not proof of audit persistence.

Per-action metadata allowlists exclude passwords, credential tokens, CSRF values, TOTP codes/secrets, full assertions, and bind credentials. Runtime cannot update/delete audit. Separate immutable export credentials strengthen tamper resistance. Workers use leases and idempotent delivery; encrypted reset notifications are erased after delivery/expiry.

Acceptance requires key rotation, restore tests, retention/holds, clock synchronization, incident response, dependency scanning, and access review. Argon2id does not establish FIPS compliance; that profile requires a validated provider and separate review. Framework defaults do not establish banking/healthcare compliance.

## References

- [OWASP password storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
- [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
- [OWASP SAML security](https://cheatsheetseries.owasp.org/cheatsheets/SAML_Security_Cheat_Sheet.html).
- [go-ldap](https://github.com/go-ldap/ldap) and [crewjam/saml](https://github.com/crewjam/saml).
