# GoStack

GoStack is a reusable Go framework for enterprise HTTP services. It provides explicit constructor injection, modular delivery/service/repository layers, PostgreSQL persistence, built-in identity management, and a CLI for creating applications and custom modules.

## Status

Phase 0: architecture design. This repository contains design documents, not an executable framework. No Go packages, migrations, or generator have been implemented.

## Design documents

- [Architecture, folder structure, and dependencies](docs/architecture.md)
- [Database and transactions](docs/database.md)
- [Modules and API](docs/modules.md)
- [Security](docs/security.md)
- [CLI and generator](docs/generator.md)
- [Delivery phases](docs/phases.md)

## Intended interface

The CLI is named `gostack`. Planned commands include `new inventory-api`, `make:model Product`, `make:crud Product`, `make:migration create_products`, `make:middleware Auth`, `migrate:up`, and `user:create-admin`.

User, role, permission, auth, session, password, MFA, LDAP, SAML, identity, and audit are built-in modules. Applications consume their public APIs; generators never recreate these modules.

## Delivery policy

Deliver one phase at a time, commit and push each completed phase to `main`, report completed files, remaining files, known issues, and the next phase, then stop until the user says `continue`.

Security and operational requirements are acceptance criteria. Suitability for regulated deployment requires implementation, verification, and deployment-specific review; Phase 0 makes no certification claim.

## License

The existing [LICENSE](LICENSE) is retained unchanged.
