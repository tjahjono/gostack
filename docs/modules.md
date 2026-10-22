# Modules and API

## Contracts

Every module has repository/service/delivery contracts, concrete structs, constructors, DTOs, wiring, and tests. Business methods use contexts, domain IDs, typed commands/results, and errors; they never expose SQL, driver rows, or HTTP requests.

Product uses `ProductRepository`, `ProductService`, and `ProductHandler`. Repository/service operations are `Create`, `Update`, `Delete`, `GetByID`, and `List`; handler exposes `RegisterRoutes`. Constructors are repository `NewPostgresRepository`, service `New`, and HTTP `New`. Updates distinguish omission from zero values; updates/deletes require expected versions.

Specialized modules expose business operations instead of arbitrary credential/audit CRUD. Verified actor values enter services from delivery; services enforce ownership and recheck privileged mutation policy. Middleware authentication alone does not authorize access.

## Responsibilities

| Module | Service/repository scope | HTTP surface under `/api/v1` |
| --- | --- | --- |
| `user` | Create/update/delete/list/get; activate/deactivate; lock/unlock; account invariants; optional initial credential hashing. | `/users`, `/users/{id}`, explicit lifecycle actions. |
| `role` | Custom-role CRUD; system-role protection; assignment/removal and provenance; last-admin protection. | `/roles`, `/users/{id}/roles`. |
| `permission` | Catalog; role/direct grants; effective-permission union; authorization checks. | `/permissions`, `/roles/{id}/permissions`, `/users/{id}/permissions`, `/users/{id}/effective-permissions`. |
| `auth` | Local login/logout; lockout; generic errors; MFA challenge issuance; current identity. | `/auth/login`, `/auth/logout`, `/auth/me`. |
| `session` | Issue/validate/rotate/revoke; own/admin listing; status and expiry enforcement. | `/sessions`, `/sessions/{id}`, `/users/{id}/sessions`, `/sessions/revoke-all`. |
| `password` | Policy/history; change; forgot/reset; token consumption; durable notifications. | `/password/change`, `/password/forgot`, `/password/reset`. |
| `mfa` | Enrollment/confirmation/disable; TOTP; recovery codes; challenge completion. | `/mfa/enrollments`, `/mfa/enrollments/{id}/confirm`, `/mfa/verify`, `/mfa/recovery-codes`, `/mfa/disable`. |
| `ldap` | Directory login, identity lookup, synchronization, group-role mapping. | `/auth/ldap/{provider}/login`, authorized `/ldap/{provider}/sync`. |
| `saml` | Metadata, requests, assertion validation, replay protection, JIT, group mapping. | `/auth/saml/{provider}/metadata`, `/auth/saml/{provider}/login`, `/auth/saml/{provider}/acs`. |
| `identity` | External identity mapping/unmapping; subject collision rejection; proof and recent authentication for linking. | `/users/{id}/identities`, `/users/{id}/identities/{identityID}`. |
| `audit` | Transactional append, authorized search/export, retention/hold workers; no update/delete API. | `/audit-logs`, `/audit-logs/export`. |

LDAP/SAML are included but disabled until valid provider configuration exists. Provider configuration never comes from an unauthenticated request. Active Directory is an LDAP adapter profile. Azure AD, ADFS, Okta, Ping, and generic SAML are documented profiles requiring fixture and interoperability verification.

Cross-module operations use repository views defined in the orchestrating module's contracts, not calls to another business service. Shared algorithms are pure policy functions. Hashing, keyring encryption, directory access, SAML verification, notifications, clock, and randomness use typed ports, struct adapters, and constructors.

## Composition

Stable registration order: audit, user, role, permission, identity, password, session, MFA, LDAP, SAML, auth, then custom modules. Construct dependencies before registration. Earlier phases register only implemented modules and never advertise missing capabilities.

Phase 3 introduces audit append persistence so identity transactions have durable events. Phase 9 completes audit search/export/retention and delivery. Minimum identity values begin with Phase 3; external behavior arrives with federation. These foundations do not claim later modules are complete.

Before Phase 4 permission enforcement exists, privileged HTTP routes deny access and are not exposed as usable administrative APIs. First-admin bootstrap completes in Phase 4 alongside role seeds.

## RBAC

Effective permissions are the distinct union of role and direct grants. No implicit deny hierarchy, role inheritance, or wildcard. Unknown keys deny. Routes require explicit permission keys; self-service additionally checks ownership. Creating permissions does not silently grant them to administrators.

- `super_admin`: every shipped permission through explicit seed grants.
- `user_admin`: approved user, role-assignment, and session administration; no super-admin grants or provider administration.
- `auditor`: audit read/export and approved account read visibility.
- `standard_user`: own profile, sessions, password, and MFA operations.

Seeds are deterministic/idempotent; privilege expansion is a reviewed migration. Actor/target checks run under locks. User administrators cannot grant outside their delegable allowlist or alter system-role grants. Permission checks initially read current database grants; caching requires versioned invalidation and new acceptance tests.

## HTTP conventions

REST is versioned at `/api/v1`; health uses `/health/live` and `/health/ready`. OpenAPI 3.0.3 is the initial format. Swagger UI uses pinned local assets and is disabled in production unless explicitly protected/enabled. SAML metadata/ACS use protocol responses.

Reject unknown JSON fields, trailing values, unsupported content types, excessive bodies, and malformed IDs. Never bind persistence models directly. Response DTOs omit hashes, digests, secrets, and internal provider attributes.

Creation uses `Location`; deletion returns 204; version conflicts 409; field violations 422; rate limits 429 with `Retry-After`; authentication/authorization 401/403. Recovery returns the same 202 response regardless of account existence. Login errors disclose neither existence nor lock state.

Errors use `application/problem+json` with stable type/code, title, status, request ID, and safe bounded field violations. SQL/internal details stay in redacted logs. Routes explicitly declare method, path, permission, and middleware; there is no reflective controller discovery.

Service tests fake repositories; handler tests use service fakes and `httptest`; repository tests use isolated real PostgreSQL schemas. External compile/contract tests verify facade use and adapter guarantees.
