# Phase 2: framework contracts and composition

## Delivered boundary

`core` supplies safe typed errors and immutable pagination requests. `core/contract` provides optional generic CRUD repository/service shapes and typed transaction callbacks. Modules still define named business-specific contracts; these building blocks do not implement business services or discover dependencies. Service methods receive a verified principal explicitly.

`security` supplies immutable principal values, typed authentication/authorization/CSRF/rate-limit ports, and constructor-created clock/randomness adapters. Context stores request identity, correlation ID, and derived client address/scheme, never dependencies. Provider implementations must be concurrency-safe and honor request cancellation. A principal constructor validates shape; only an authentication provider can establish identity from credentials.

## Explicit application composition

`framework.New` retains the Phase 1 startup factory and accepts at most one optional `framework.Options` value. `Options.Modules` contains already constructed custom module descriptors; `Options.HTTP` contains HTTP policy and provider dependencies. No registration happens through imports, global variables, reflection, or a service locator.

Applications owning domain repositories construct their pool, repositories, services, handlers, descriptors, and security-provider adapters explicitly. `framework.NewReadiness` combines their database lifecycle and migration verifier. Pass those dependencies to `framework.NewWithDependencies`, including the constructed modules in `Options`. The caller owns cleanup if this constructor returns an error; a successfully constructed application owns shutdown. The `framework.New` factory cleans up resources if its own construction fails.

The [external consumer fixture](../tests/consumer/testdata/consumer_test.go.txt) is a complete executable test of this public API. Its test harness creates a separate Go module with a local replacement, compiles it, sends an HTTP request, and constructs/closes an application. It imports no internal packages.

## Modules and registry

`moduleapi` is a leaf package containing delivery, lifecycle, and module contracts. `NewDescriptor` validates/copies names, dependencies, and permission keys. `moduleapi.New` combines a descriptor with explicit delivery and lifecycle implementations. Modules without workers use a separately constructed `PassiveLifecycle`.

`internal/module/registry.go` is exposed through a public framework facade. It freezes descriptor metadata and route declarations during construction. Stable topological ordering loads framework modules before custom modules. Framework-to-custom dependencies, missing prerequisites, cycles, reserved custom names, duplicate names/permissions, unknown route permissions, invalid route patterns, and overlapping patterns that ServeMux rejects are startup errors. Custom business routes must use `/api/v1`. Metadata and route accessors return copies; there is no service lookup.

Only the health module is built in at this milestone. Authentication, user, and session modules are not represented by empty implementations. They arrive in Phase 3; other identity modules retain their planned phases.

Start initializes modules in dependency order before readiness is enabled. Initialization failure cleans up the failing module and previously started modules in reverse order, including partial initialization. Stop continues reverse cleanup after individual failures. Lifecycle panics become safe initialization/cleanup errors. Start/Stop operations are serialized; cancelled callers do not wait indefinitely for another operation.

Lifecycle callbacks must cooperate with their contexts. Start's context bounds initialization, not a worker's entire lifetime: workers own cancellation and terminate in Stop. HTTP requests drain before modules stop, and the database closes afterward. Go cannot forcibly terminate a non-cooperative user callback.

## Route policy and middleware

Routes declare a method, path, handler, and explicit `Public`, `Authenticated`, or `Permission` access. The zero policy is invalid. Permission keys use `resource:action`, with no wildcard. Unsafe methods also require a valid rate bucket, limiter, explicit allowed origin, and CSRF verifier. Protected routes require authentication; permission routes require an authorizer. Missing providers fail construction. No permissive provider is installed by default.

Processing order is a fresh 128-bit request ID, panic recovery, trusted-proxy derivation, secure headers/body limits/CORS, route rate limiting, session authentication, applicable CSRF verification, permission checks, and delivery. Request context deadlines bound cooperative providers and handlers. The standalone stack defaults to 15s; an application defaults to three quarters of its HTTP write timeout. An explicit request deadline must be shorter than that write timeout.

Default body limit is 1 MiB, configurable from one byte through 64 MiB. Known excessive lengths fail immediately; `MaxBytesReader` bounds streamed bodies. `DecodeJSON` rejects unknown fields, trailing values, wrong content types, and oversized content. `WriteError` maps safe typed errors to problem JSON without internal causes. ResponseController, flushing, and HTTP/1 hijacking remain accessible through response wrappers. A panic after response commitment aborts the response instead of appending a misleading JSON error.

Trusted proxy networks are explicit `netip.Prefix` values; trust-all prefixes are rejected. Only trusted peers can supply `X-Forwarded-For` and `X-Forwarded-Proto`. Traverse chains from the nearest trusted side, stopping at the first untrusted hop. Reject malformed/multiple chains and unsupported `Forwarded` headers from trusted peers. Untrusted peer headers cannot change client identity or HTTPS status. Scheme/host is never inferred from arbitrary Host input for origin policy.

CORS uses exact configured origins and credentials, never wildcard origins. Preflight permits the supported HTTP methods and only Content-Type/X-CSRF-Token headers. CSRF requires a configured origin and a provider-verified token/binding. Session cookies are read from `__Host-gostack_session`; pre-authentication CSRF binding uses `__Host-gostack_csrf`. Credential issuance and verification implementations arrive in their owning phases. Public routes clear any ambient principal; protected routes replace it with provider-verified identity. Duplicate session cookies are rejected.

Denied credentials/permissions/CSRF produce generic 401/403 responses. Provider failures produce 503; rate rejection produces 429 with bounded Retry-After. Panic details, tokens, and provider errors never appear in those responses. CSP, nosniff, frame/referrer/permissions policies apply by default; HSTS requires an actual or trusted-proxy HTTPS scheme. Protocol-specific SAML CSRF handling and Swagger CSP adjustments remain in their implementation phases.

## Transactions

`database.NewPostgresTransaction` accepts a pool, typed repository binder, transaction options, and bounded cleanup timeout. `Within` supplies repository interfaces to the callback. Services import only `core/contract.Transaction[Repositories]`; SQL handles remain in the repository binder. Commit follows a successful, uncancelled callback. Errors, cancellation, and panics trigger bounded rollback; panics propagate after cleanup. No external effects, retained transaction-bound repositories, automatic retries, or transaction state in context are allowed.

## Verification

The phase includes registry/lifecycle, HTTP policy/proxy/CORS/CSRF/rate-limit, body/error/recovery/deadline, routing conflict, principal/pagination, and architecture tests. Real PostgreSQL tests verify commit, business-error rollback, panic rollback, cancellation, and connection release, alongside the Phase 1 integration suite. Normal and race-enabled suites, vet, build, and module verification are the delivery gates.

Architecture checks reject mutable production package variables and init functions, direct forbidden layer imports, driver types in business contracts/models, internal imports of public module facades, and bootstrap imports of its public factory. These enforce explicit source boundaries; they do not replace a semantic security/architecture review. Docker testing remains skipped at the user's request.

## References

- [Go ServeMux and pattern conflicts](https://pkg.go.dev/net/http#ServeMux).
- [Go ResponseController and response wrappers](https://pkg.go.dev/net/http#ResponseController).
- [Go forwarded-header trust considerations](https://pkg.go.dev/net/http/httputil#ReverseProxy).
- [OWASP CSRF prevention](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

## Next phase

Phase 3 implements user, local authentication, sessions, lockout, durable audit append, and their PostgreSQL schema. No business identity endpoint is claimed by Phase 2. Wait for `continue` before proceeding.
