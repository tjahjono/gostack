# CLI and generator design

## Commands

| Command | Behavior |
| --- | --- |
| `gostack new inventory-api` | Standalone application, explicit wiring/registry, built-ins, Docker/PostgreSQL, migrations, health, OpenAPI/Swagger, config example, build/test targets. Optional `--module` sets its import path. |
| `gostack make:model Product` | Interactive name, fields/types/constraints, migration and CRUD choices. Noninteractive mode accepts a validated field manifest. |
| `gostack make:crud Product` | Read model AST/types; generate all layers, DTOs, routes, wiring, tests, optional migration. Refuse unsupported model shapes. |
| `gostack make:migration create_products` | Paired timestamped application migrations from structured operation/schema input. |
| `gostack make:middleware Auth` | HTTP middleware contract, struct, constructor, behavior test; no replacement of built-in auth. |
| `gostack migrate:up` | Verify checksums; acquire deployment lock; framework then application migration streams; report versions. |
| `gostack migrate:down --steps N` | Explicit rollback, application stream by default; reject framework rollback breaking application dependencies. |
| `gostack user:create-admin` | First-admin bootstrap with protected input, account policy, invariant lock, and audit. |

Commands have deterministic exit codes and actionable errors. Generators support `--dry-run` and `--force`. Dry-run validates and reports files, AST edits, conflicts, schema operations, and versions without writing, hooks, or database access. Force replaces only explicitly targeted generated files/declarations; never arbitrary user files, secrets, applied/committed migrations, or project roots. Detect refusals before writing.

Reject reserved framework model names case-insensitively after normalization/singularization: User, Role, Permission, Auth, Session, Password, MFA, LDAP, SAML, Identity, Audit and their tables. Model/CRUD commands reject Go keywords, invalid/colliding identifiers, separators, traversal, absolute paths, and registered module collisions. Middleware uses a separate validated namespace.

## Field representation

Use typed generation metadata: module/import path, model/table names, fields, Go/SQL types, nullability, defaults, constraints/indexes, API exposure, and validation. Initial fields: string, bool, signed integer, exact decimal, UUID, timestamp, date, nullable variants. Choose/pin the exact-decimal adapter during implementation; money never uses binary floating point.

Constraints use typed options, not arbitrary SQL, imports, tags, or executable template fragments. Relationships/enumerations require validated representations. Default names use snake-case plurals with an explicit override. Show resolved names. Sensitive fields are explicitly excluded from responses.

## Execution pipeline

1. Parse `go.mod`; validate layout, names, paths, registry AST/types, and template version.
2. Build an immutable plan; detect all conflicts, including case-insensitive filesystems and both migration paths.
3. Template new files and parse them. Modify existing Go files through AST operations; add imports/declarations structurally, never by string replacement.
4. Stage on the destination filesystem. Run formatting, parsing/type checks, and focused generated tests with deadlines against locked dependencies. Never silently run arbitrary hooks or template commands.
5. Acquire a project generation lock, recheck source hashes/conflicts, and publish validated outputs. Journal the multi-file operation durably; back up permitted force replacements. Recover/rollback interrupted publication before another command proceeds.
6. Report paths and verification. Failure preserves the original project or reports recovery state; do not claim a multi-file filesystem transaction.

Use `text/template` only for new source documents. Existing-source edits use `go/parser`, `go/ast`, `go/token`, `go/format`; semantic analysis uses `go/types` or a pinned packages adapter. Reject symlink/reparse-point escapes after resolving ancestors. Outputs stay inside the project root.

Without CRUD, `make:model` emits model/DTO definitions and meaningful model tests. With migration, emit concrete validated DDL. `make:crud` emits the complete architecture module template plus bootstrap/registry edits and integration tests. Migration commands collect create/alter/drop details and emit real paired DDL; refuse unsupported/irreversible operations rather than creating empty SQL or placeholders.

## Registration and wiring

The application's `internal/module/registry.go` explicitly accepts constructed custom modules. Add imports, typed parameters, and registration statements through AST nodes while preserving framework-first order. Bootstrap AST edits construct repository, service, handler, descriptor in that order. No runtime discovery/reflection.

Unsupported customized registry/bootstrap shapes cause a clear refusal with exact manual integration instructions. The descriptor accepts a leaf delivery interface and never imports its child layers, avoiding module/model cycles.

## Generated behavior and tests

CRUD output has constructors/interface assertions, typed validation, bounded pagination, parameterized PostgreSQL, affected-row checks, optimistic versions, typed errors, and safe DTOs. Routes declare permission keys and deny until grants are configured. Permission seed changes require explicit operator review; no accidental public routes.

Service tests cover validation/error propagation using repositories; handler tests cover binding, actor inputs, redaction, and error mapping using service fakes. Real-PostgreSQL repository tests cover round trips, uniqueness, conflicts, rollback. Application integration tests exercise routing/middleware/persistence. Missing database configuration skips local integration with an explicit reason but fails the required CI integration job.

Generator tests cover golden plans, AST preservation, malformed source, collisions, reserved names, unsupported fields, dry-run immutability, force bounds, crash recovery, Windows paths, and external app compilation. Run normal/race suites on generated applications, not only template tests.

## Project assets

`new` emits a multi-stage non-root Dockerfile, graceful shutdown, read-only-compatible runtime, compose PostgreSQL with persistence/readiness, explicit migration service, `.env.example` without usable secrets, Makefile, OpenAPI, and pinned local Swagger assets. Built-in wiring is automatic; never generate User or Role.

The initial `new` command arrives in Phase 10 with health specification/docs mounting. Phase 11 completes the API contract and Swagger integration. Migration assets include checksums and ownership metadata; upgrades add reviewed assets rather than overwriting applied SQL or user code.
