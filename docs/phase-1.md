# Phase 1: infrastructure and runtime

## Implemented boundary

Public packages `config`, `logging`, `database`, `migration`, and `framework` provide interfaces, concrete implementations, and explicit constructors. `internal/bootstrap` assembles infrastructure without importing `framework`. There is no application singleton, service lookup, implicit environment loading, or automatic schema migration.

The minimal executable supports `serve`, `migrate up`, `migrate down <app|framework> <steps>`, and `healthcheck [url]`. These demonstrate the framework; the `gostack` CLI remains Phase 10 work.

## Pinned baseline

| Component | Version |
| --- | --- |
| Go | 1.27.3 |
| pgx | v5.11.0 |
| golang-migrate | v4.20.1 |
| PostgreSQL | 18.6, Alpine 3.24 container variant |
| Runtime image | distroless static Debian 12, non-root, immutable digest in Dockerfile |
| CI | Ubuntu 24.04 and Windows 2025; action commits pinned in workflow |

`go.sum` locks module contents. Dockerfile/Compose include resolved image digests. `.gitattributes` fixes LF endings, including SQL, so migration checksums are stable across Windows and Linux checkouts.

## Configuration and logging

Construct `config.NewEnvProvider(os.LookupEnv)` and call `Load`. Configuration defaults to production. Database URLs must explicitly select `verify-full` in production or `disable`/`verify-full` in development/test. Ambiguous/unsupported URL parameters are rejected. Secrets redact formatting, JSON, and slog values; only `Reveal` exposes the underlying credential to connection code.

Default budgets: 10 database connections; 5s connection/query timeout; 1s lock timeout; 30m connection lifetime; 2m migration operation; HTTP read 15s, headers 5s, write 30s, idle 60s, shutdown 15s, health 2s; 16 KiB headers. Every budget is validated. Database and HTTP duration overrides use the named `GOSTACK_DB_*`, `GOSTACK_HTTP_*`, and `GOSTACK_MIGRATION_TIMEOUT` keys defined in `config/config.go`.

JSON logging uses the injected writer/level and redacts sensitive attribute keys, including nested groups. Standard HTTP transport errors use an injected logger adapter. Never pass revealed credentials as free-form log messages; key-based redaction cannot identify arbitrary secrets in arbitrary text.

## PostgreSQL role provisioning

Create database `gostack` as a database administrator. Set `POSTGRES_USER`, `POSTGRES_DB=gostack`, `GOSTACK_MIGRATOR_PASSWORD`, and `GOSTACK_RUNTIME_PASSWORD`; standard PostgreSQL connection variables select the server. Run `sh deploy/postgres/init.sh` once with `psql` available. The script reads password variables inside psql rather than putting passwords in process arguments. It fails on existing roles instead of silently changing them.

The migrator can create objects in `public`. The runtime can connect, use the schema, and read migrator-owned tables; it cannot create objects or modify version/checksum records. Later feature migrations add table-specific DML grants. For custom role/database names, provision equivalent grants explicitly; the supplied convenience script intentionally targets the documented names.

Startup rejects elevated runtime roles, membership in elevated roles, schema creation, and ownership of public tables. Production TLS verifies the server and uses configured root certificates. Pool connections set UTC, a controlled search path, and bounded statement/lock/idle-transaction timeouts. Pool shutdown reports an expired caller deadline while outstanding acquisitions drain.

## Migration semantics

Both `migrations/framework` and `migrations/app` must exist. The framework baseline creates the checksum ledger; the application stream is empty in this reusable framework repository. Names are fourteen-digit UTC timestamps followed by a snake-case slug and paired `.up.sql`/`.down.sql` suffixes. Files are nonempty regular files, limited to 1 MiB each.

The runner injects source/database instances directly into golang-migrate without global driver registration. A database-wide advisory lock covers both streams. Each migration's SQL and ledger insertion/removal commit in one PostgreSQL transaction. golang-migrate persists dirty state separately before execution; failure or an ambiguous interruption requires operator review. No automatic force/repair exists.

Checksums include version, name, and both SQL directions. Verify all applied checksums before any new migration. Refuse unknown versions, missing ledger entries, extra ledger entries, modified rollback SQL, excessive rollback steps, and framework rollback while application migrations remain. Readiness performs a read-only snapshot check against the exact expected heads.

SQL must be transaction-compatible and must not include explicit transaction control. Nontransactional operations are unsupported in this phase and require a separately reviewed extension. Down migrations are explicitly invoked and can destroy data; back up and review them before use. Never clear a dirty marker until database changes and ledger state have been reconciled by an operator.

## Lifecycle

Construction connects, verifies the restricted runtime role, and checks schema compatibility before opening HTTP. The application runs once. Readiness is withdrawn before drain. Active requests finish within the shutdown deadline, then forced closure bounds an overrun. Database resources close after request drain. Liveness/readiness responses never disclose database errors.

Only health routes are present. Module registration, middleware composition, trusted proxy policy, business routes, and user management are later phases. This milestone is not a complete enterprise security stack.

## Verification evidence

Executed locally with PostgreSQL 18.6 on loopback, independent random database passwords, isolated temporary test databases, and a portable GCC compiler:

- `go test -count=1 ./...`, with integration configuration required.
- `go test -race -count=1 ./...`, with the same real PostgreSQL tests.
- `go vet ./...`, `go mod verify`, and `go build ./...`.
- Explicit migration application using the minimal executable.

Tests cover redaction, unsafe configuration, cancellation, readiness disclosure, active-request drain, runtime-role restrictions, bounded pool closure, concurrent deployments, rollback dependency/count checks, checksum tampering, atomic failed DDL, dirty-state preservation, and lock deadlines.

Docker image builds and execution tests were skipped at the user's request. Their success is not claimed. CI contains normal/race/build jobs plus a PostgreSQL integration job; remote CI results are separate from the local evidence above.

## Next phase

Phase 2: core framework contracts, module registry, constructor wiring, middleware foundation, and architecture checks. Implementation begins only after `continue`.
