# Database design

## Conventions

PostgreSQL is authoritative. IDs are application-generated cryptographically random UUIDs. Timestamps use UTC `timestamptz`; domain models use framework ID values and `time.Time`, not driver types. Mutable aggregates have creation/update timestamps and a positive optimistic version. Updates match the expected version or return conflict.

Foreign keys, check constraints, and unique indexes enforce invariants independently of services. Index foreign keys used for lookups. Parameterize all queries; allowlist sort keys/operators. No client-supplied SQL.

Usernames use a documented ASCII identifier alphabet and lowercase normalized keys. Email normalization lowercases domain and local part as an explicit account policy; internationalized addresses await a tested normalization policy. Preserve display forms. Database constraints validate normalized keys. Uniqueness includes deleted users.

## Tables

Each row lists additional columns beyond the primary key. UUID foreign keys reference the named table unless a historical reference is specified. Exact DDL is delivered in implementation phases.

| Table and primary key | Columns and invariants |
| --- | --- |
| `users(id)` | Username/key, email/key, display name, status (`active`, `inactive`, `deleted`), nullable password hash/change time, must-change-password flag, failed attempts/window start, administrative-lock flag, nullable timed-lock expiry, credential/session/authorization versions, nullable deletion time, timestamps/version. External-only accounts have no password hash. |
| `roles(id)` | Unique immutable key, name, description, system flag, timestamps/version. Seed `super_admin`, `user_admin`, `auditor`, `standard_user`; no default credentials. |
| `permissions(id)` | Unique `resource:action` key, description, system flag, timestamps/version. No wildcard bypass. |
| `user_roles(user_id, role_id, source, source_key)` | Source (`local`, `ldap`, `saml`), source key empty only for local grants, nullable granting actor, creation time. Preserve provenance. |
| `role_permissions(role_id, permission_id)` | Nullable granting actor, creation time, reverse-lookup indexes. |
| `user_permissions(user_id, permission_id)` | Direct additive grants, nullable granting actor, creation time. |
| `sessions(id)` | User ID, unique 32-byte token digest, CSRF digest, authentication method/assurance, credential/session version snapshots, issue/activity times, idle/absolute expiry, nullable revocation time/reason, nullable IP, bounded user agent. No raw token. |
| `mfa_methods(id)` | User ID, kind, label, encrypted secret, key ID/format version, pending/active state, enrollment expiry, nullable confirmation time, last accepted TOTP counter, timestamps/version. Initially one active TOTP method per user. |
| `mfa_recovery_codes(id)` | User ID, unique digest, generation batch, nullable consumption time, creation time. Raw codes are displayed once. |
| `password_history(id)` | User ID, encoded Argon2id hash, creation time; index user and descending creation time. |
| `password_reset_tokens(id)` | User ID, unique digest, issuance credential version, expiry, nullable consumption time, creation time. |
| `external_identities(id)` | User ID, protocol, configured provider key, immutable subject, nullable display attributes, last synchronization time, timestamps/version. Unique `(protocol, provider_key, subject)`. |
| `audit_logs(id)` | Event time, request ID, nullable historical actor/subject/session IDs, action, target type/ID, outcome, reason code, nullable IP, allowlisted JSON metadata, schema version. Append-only. |
| `saml_assertion_replay(provider_key, assertion_id)` | Expiry, creation time, user ID. Unique insertion before authentication completes. |
| `auth_challenges(id)` | User ID, kind, unique token digest, initial authentication method, credential/session snapshots, expiry, attempt count/limit, nullable consumption time, creation time. No application access. |
| `saml_auth_requests(id)` | Provider key, unique request ID, unique browser-correlation digest, allowlisted return path, expiry, nullable consumption time, creation time. |
| `security_rate_limits(scope, key_digest, window_start)` | Atomic shared counter and expiry for multiple replicas. |
| `notification_outbox(id)` | Message type, recipient reference, encrypted bounded payload, key ID, creation/expiry, attempts, next attempt, nullable delivery time, lease owner/expiry. Erase reset-link payload after delivery. |
| `audit_retention_holds(id)` | Time range, optional actor/subject scope, reason reference, issuing actor, creation time, nullable release time. Retention excludes matching held records. |
| `gostack_migration_checksums(stream, version)` | Immutable content digest and installation time. |

Extra tables support direct grants, challenges, browser correlation, shared throttling, reliable notifications, legal holds, and migration integrity. `golang-migrate` additionally owns a version/dirty table per migration stream.

## Relationships and deletion

Users have many sessions, factors, recovery codes, password records, identities, direct permissions, and role grants. Roles have permissions through `role_permissions`. External grants retain provider origin; synchronization never removes local grants.

Deletion is a service transaction: mark deleted, scrub configured profile fields, remove mappings/grants, erase credential material and challenges, revoke sessions, increment versions, and append audit. Reserve normalized identifiers. Historical audit references have no cascading foreign keys; audit survives account deletion.

Role/permission deletion is restrictive until the service removes dependent grants and invalidates affected users in the same transaction. System roles cannot be deleted. A serialized administrative invariant lock prevents removing/deactivating the last active `super_admin`; bootstrap takes the same lock.

## Atomic operations

| Use case | Required transaction |
| --- | --- |
| Failed login | Lock account; update attempts/observation window and timed lock; append failure audit. Unknown accounts consume shared limits and sanitized audit. |
| Successful login | Revalidate account/credential versions and locks; clear attempts; create session or bounded MFA challenge; append audit. |
| Session use/revocation | Validate current account and version snapshots plus both expiry limits; revocation/user-wide version changes invalidate subsequent validations across replicas. |
| Password change/reset | Lock user; recheck history/version; consume reset token if applicable; replace hash; append/prune history; revoke sessions/challenges/reset tokens; append audit. |
| MFA verification | Lock challenge/method; consume strictly newer valid counter or unused recovery code; enforce limits; consume challenge; create session; append audit. |
| MFA enrollment/disable | Verify recent authentication; update factor/recovery state; revoke affected sessions/challenges; append audit. |
| RBAC change | Revalidate actor authority under locks; update grants; increment affected authorization versions; append audit. |
| LDAP sync | Apply bounded batch; reconcile provider-owned mappings/grants only; revoke confirmed disabled identities; append audit. |
| SAML ACS | Consume pending request; insert replay ID; resolve/JIT provision identity; reconcile mapped grants; create session/challenge; append audit. Roll back together. |
| Forgot password | Invalidate earlier tokens; create digest; enqueue encrypted notification; append audit. Public result also succeeds for unknown accounts. |

Lock order is administrative invariant, users by ID, roles/permissions by ID, then credential/session/challenge rows by ID. Retry serialization/deadlock errors with bounded jitter, never policy errors. Network effects are outside retriable transactions.

Session validation linearizes at its database check; requests authorized before revocation may finish. Sensitive writes revalidate session and authorization in their mutation transaction. Authoritative database time governs expiry checks; tests inject a clock at policy boundaries.

## Indexes and retention

Unique indexes cover normalized users, role/permission keys, token digests, external subjects, and replay IDs. Composite indexes cover user sessions/expiry, challenges, grants, identities, audit `(event_time, id)` and actors/subjects, and outbox polling.

Partial indexes use stored state, never the current time. Queries reject expired records before cleanup. Bounded workers remove expired challenges, reset tokens, rate windows, delivered payloads, and old revoked sessions. Retain replay entries through assertion expiry plus clock skew. Audit retention/export respect persistent legal holds.

Lists use stable authenticated cursors, typed bounded filters, default 25 and maximum 100 records. No unrestricted counts.

## Migrations and operations

Framework and application streams use `gostack_schema_migrations` and `app_schema_migrations`. One deployment advisory lock protects execution; framework precedes application. Verify checksums before applying and at startup. Application migrations cannot alter framework tables without a reviewed extension.

Use increasing unique UTC timestamps and paired `.up.sql`/`.down.sql` files. Versions exceed the stream maximum. Migrations declare transaction behavior; nontransactional operations are isolated and documented. Dirty state blocks progress. Rollback is explicit and reviewed, never an automatic startup action. Expand/backfill/contract changes preserve rolling upgrades; destructive changes require restore evidence.

Separate runtime, migration, retention, and export roles. Runtime audit rights permit insert/select only. Production database TLS verifies the server. Bound pool size, lifetime, statement/lock timeouts, and deadlines. Integration tests use real PostgreSQL and exercise races and rollback.
