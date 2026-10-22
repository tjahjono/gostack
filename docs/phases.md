# Delivery phases

## Protocol

Complete exactly one authorized phase, verify its scope, review the diff/secrets, commit, push to `main`, report results, and stop. A failed push preserves the local commit and is reported honestly. Only `continue` authorizes the next phase.

Every delivered file is written to the repository and included in the response with its path and complete content. Phase 0 is documentation only. Later phases deliver compiling implementations and meaningful tests without pseudocode, unfinished markers, or placeholder migrations.

## Deliverables and gates

Paths are implementation targets. Each module target expands to all layers/tests in `architecture.md`, its public facade, and applicable migrations.

| Phase | Files and scope | Acceptance |
| --- | --- | --- |
| 0 | `README.md`, `docs/architecture.md`, `docs/database.md`, `docs/modules.md`, `docs/security.md`, `docs/generator.md`, `docs/phases.md`. | Consistent reuse, dependency, schema, transaction, module, security, and generator design; no implementation. |
| 1 | `go.mod`, `go.sum`, `config/`, `logging/`, `database/`, `migration/`, startup/lifecycle in `framework/`, `internal/bootstrap/`, minimal example executable, migration assets, Dockerfile, compose, Makefile, `.env.example`, CI. | Compile; config validation/redaction; bounded pool lifecycle; migration locks/checksums/dirty handling; graceful shutdown; Docker/PostgreSQL smoke test. |
| 2 | `moduleapi/`, `httpkit/`, module/DI contracts, `internal/module/registry.go`, transaction/security ports, middleware, architecture tests. | Explicit constructors; order/duplicate/cycle validation; prohibited imports; HTTP/proxy limits; external-consumer compile test. |
| 3 | Full user/auth/session modules/facades; minimal identity values; Argon2id adapter; local password policy; account/session/rate-limit/audit append migrations and adapters. | CRUD/lifecycle; login/logout/lockout; sessions/revocation/CSRF; audit rollback; PostgreSQL concurrency. Privileged routes remain closed pending RBAC. |
| 4 | Full role/permission modules/facades; grant migrations/seeds; permission middleware; first-admin bootstrap command. | Direct/effective grants; delegation limits; last-admin guard; immediate permission changes; escalation tests. |
| 5 | Full password module/facade; history/reset/outbox schema; notification adapter; shared history enforcement. | Enumeration-safe recovery; single-use races; history; revocation; encrypted durable notification/retries. |
| 6 | Full MFA module/facade; AEAD keyring; factor/challenge/recovery schema; auth wiring. | TOTP vectors; enrollment/disable; atomic counter/code consumption; expiry/attempt limits; recovery/assurance tests. |
| 7 | Full LDAP/identity modules/facades; external identities; LDAP/AD profiles and sync. | TLS/StartTLS; empty-bind rejection; immutable mapping; safe reconciliation; outage/escalation tests. |
| 8 | Full SAML module/facade; request/replay schema; metadata/login/ACS/JIT/provider profiles. | Signature/claim/request/browser validation; replay races; hostile XML; key rotation; interoperability evidence. |
| 9 | Full audit module/facade; search/export/retention/hold delivery and workers; event catalog. | Required actions, transaction coupling, redaction, append-only grants, bounded export/holds. |
| 10 | `cmd/gostack/`, CLI, generators/templates, command docs; new/model/CRUD/migration/middleware; integrated migration/admin commands. | Generated external app compiles; AST-only edits; no overwrite; force/dry-run/crash recovery; valid SQL; generated tests. |
| 11 | `api/openapi.yaml`, local Swagger assets/mount, generated spec integration. | Spec validation/route parity; auth/errors/pagination; protected production docs. |
| 12 | Integration/contract/security suites, generator fixtures, CI matrix, testing docs. | `go test ./...`, `go test -race ./...`, fuzzing, real PostgreSQL, LDAP/SAML negatives/interoperability, clean generation. |
| 13 | Threat model, configuration/dependency hardening, SBOM checks, operational runbooks. | Hash/load/rate benchmarks; rotation/restore; reviewed trust boundaries; no unresolved release blockers. |
| 14 | Architecture audit, API/support policy, debt register, release checklist, roadmap, example review. | Prior gates pass; dependency/API review; reproducible release evidence; remaining risks recorded. |

Security and tests accompany each feature. Phase 12 expands cross-cutting tests; Phase 13 strengthens existing controls. Intermediate phases are development milestones, not production-complete releases.

## Phase 0 report

Completed: the seven Phase 0 files. Existing `LICENSE` and `.gitignore` are unchanged.

Remaining: all Phase 1-14 implementation files, module layers/facades, SQL, CLI/templates, generated assets, OpenAPI, tests, and CI.

Known issues: no executable framework, compilation, runtime verification, protocol interoperability, or deployment evidence exists yet. Exact supported dependency/toolchain/container versions and operational policies are selected/tested in their owning phases. Licensing remains unchanged. Multi-tenancy, WebAuthn/passkeys, and alternative primary database adapters are future work.

Next: Phase 1 bootstrap, configuration, logger, PostgreSQL, migrations, Docker, startup. Wait for `continue`.
