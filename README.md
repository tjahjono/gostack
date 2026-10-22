# GoStack

GoStack is a reusable Go framework for enterprise HTTP services, built around explicit constructor injection and delivery/service/repository layers.

## Status

Phases 1 and 2 are implemented: configuration, logging, PostgreSQL, migrations, runtime lifecycle, typed contracts, immutable module registration, transaction boundaries, HTTP security middleware, and architecture checks. Identity modules and the `gostack` generator CLI are delivered in later phases. The executable under `examples/minimal` demonstrates framework composition; it is not the framework's product CLI.

## Requirements

Go 1.27.3 or newer, PostgreSQL 18.6 for the tested database baseline, and separate runtime/migration database roles. Race tests require a supported C compiler. Makefile targets require a POSIX-compatible shell.

## Native execution

Provision database `gostack` and the roles/privileges described in [Phase 1 operations](docs/phase-1.md). Export these environment variables; the framework does not implicitly load `.env` files:

```text
GOSTACK_ENV=development
GOSTACK_DATABASE_URL=postgres://gostack_runtime:<URL-encoded-password>@127.0.0.1:5432/gostack?sslmode=disable
GOSTACK_MIGRATION_DATABASE_URL=postgres://gostack_migrator:<URL-encoded-password>@127.0.0.1:5432/gostack?sslmode=disable
```

```sh
go run ./examples/minimal migrate up
go run ./examples/minimal serve
go run ./examples/minimal healthcheck http://127.0.0.1:8080/health/ready
```

Startup rejects privileged runtime credentials, pending/dirty/unknown migrations, and checksum changes. `/health/live` reports process health; `/health/ready` checks PostgreSQL and migration state. Production is the default configuration profile and requires database `sslmode=verify-full`; plaintext local connections require an explicit development/test profile.

## Docker assets

Copy `.env.example` to `.env` and supply three independent random hexadecimal passwords. Compose reads that file for interpolation; it is not a native application configuration loader.

```sh
docker compose up --build -d api
docker compose down
```

Images and CI actions are pinned by digest/commit. The runtime is non-root with a read-only filesystem in Compose. Docker execution testing was skipped at the user's request.

## Verification

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

For PostgreSQL integration tests, supply `GOSTACK_TEST_DATABASE_URL` for an isolated test administrator and `GOSTACK_TEST_RUNTIME_DATABASE_URL` for the migrated, restricted runtime role. Set `GOSTACK_REQUIRE_INTEGRATION=true` to fail instead of skipping when either URL is absent. The integration administrator must be able to create/drop temporary databases; never point it at a production cluster.

## Design and delivery

- [Architecture and dependencies](docs/architecture.md)
- [Database and transactions](docs/database.md)
- [Modules and API](docs/modules.md)
- [Security](docs/security.md)
- [CLI and generator](docs/generator.md)
- [Delivery phases](docs/phases.md)
- [Phase 1 operations and verification](docs/phase-1.md)
- [Phase 2 module composition and HTTP policies](docs/phase-2.md)

User, role, permission, auth, session, password, MFA, LDAP, SAML, identity, and audit are built-in framework modules planned for the following phases. Generators will never recreate them.

Complete one phase, commit and push to `main`, report its files, remaining work, known issues, and next phase, then wait for `continue`.

## License

The existing [LICENSE](LICENSE) is retained unchanged.
