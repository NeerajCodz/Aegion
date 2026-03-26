# Aegion - Identity and Access Platform

> One container. One port. One config file. Complete auth.

Aegion is a self-hosted identity and access platform. It replaces Auth0, the Ory stack, and Supabase Auth with a single Go binary you own and run yourself.

## Quick Start

```bash
# Clone and start
git clone https://github.com/aegion/aegion
cd aegion
docker-compose up -d

# Open admin panel
open http://localhost:8080/aegion
```

## Documentation

See [docs/](./docs/) for complete documentation:

- [Overview](docs/overview.md) — High-level narrative and positioning
- [Architecture](docs/architecture.md) — Go+Rust internals and runtime flow
- [Security](docs/security.md) — Authentication and security controls
- [Configuration](docs/config.md) — `aegion.yaml` model and ownership split

## Project Status

**Phase 1 — Core Identity Platform** (In Development)
- [ ] Core identity model
- [ ] Password authentication
- [ ] Sessions
- [ ] Email OTP & Magic link
- [ ] Admin panel
- [ ] Postgres persistence
- [ ] Identity schemas
- [ ] Courier (email/SMS delivery)

## License

[MIT](LICENSE)
