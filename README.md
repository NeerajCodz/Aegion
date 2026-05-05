# Aegion - Identity and Access Platform

[![CI](https://github.com/Astraive/Aegion/actions/workflows/ci.yml/badge.svg)](https://github.com/Astraive/Aegion/actions/workflows/ci.yml)

> One container. One port. One config file. Complete auth.

Aegion is a self-hosted identity and access platform. It replaces Auth0, the Ory stack, and Supabase Auth with a single Go binary you own and run yourself.

## Quick Start

```bash
# Clone and start
git clone https://github.com/Astraive/Aegion
cd Aegion
docker-compose up -d

# Initialize admin user (first time only)
docker-compose exec aegion aegion bootstrap --admin-email admin@example.com

# Open admin panel
open http://localhost:8080/aegion
```

## Architecture

Aegion combines Go and Rust components in a single deployment:
- **Core API** (Go): Identity management, authentication, sessions
- **Security Engine** (Rust): Policy enforcement, cryptographic operations
- **Admin Panel** (Web): Identity management interface
- **Unified Configuration**: Single `aegion.yaml` file controls all modules

See [docs/architecture.md](docs/architecture.md) for detailed internals.

## Development

See [docs/development.md](docs/development.md) for local development setup, testing, and contribution guidelines.

## Contributing

We welcome contributions! Please:
1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Follow our [development guidelines](docs/development.md)
4. Submit a pull request

## Documentation

See [docs/](./docs/) for complete documentation:

- [Getting Started](docs/getting-started.md) — Prerequisites and setup
- [API Reference](docs/api-reference.md) — Complete API documentation
- [Development](docs/development.md) — Local development and testing
- [Deployment](docs/deployment.md) — Production deployment guide
- [Overview](docs/overview.md) — High-level narrative and positioning
- [Architecture](docs/architecture.md) — Go+Rust internals and runtime flow
- [Security](docs/security.md) — Authentication and security controls
- [Configuration](docs/config.md) — `aegion.yaml` model and ownership split
- [Release Maturity Matrix](docs/release-maturity-matrix.md) — GA/Beta/Not-GA truth source

## Project Status

**Hybrid-first release train (active)**
- Source of truth for module maturity is `build/release-maturity.json` and [docs/release-maturity-matrix.md](docs/release-maturity-matrix.md).
- Current module mix is intentionally uneven: core/password/magic_link are stable, while standalone surfaces advance by maturity gate.

## License

[MIT](LICENSE)
