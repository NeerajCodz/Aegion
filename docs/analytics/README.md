# Aegion Analytics Documentation

Welcome to the Aegion Analytics System documentation. This comprehensive guide covers everything you need to know to deploy, configure, and integrate with the analytics system.

## 📚 Documentation Index

### Quick Start
- **[Quick Start Guide](quickstart.md)** - Get up and running in 5 minutes
- **[Setup & Deployment Guide](setup.md)** - Local, Docker, and Kubernetes deployment

### API Documentation
- **[REST API Specification](openapi.yaml)** - Complete OpenAPI 3.0 spec
- **[REST API Usage Guide](api.md)** - How to query events, export data, and use REST endpoints
- **[GraphQL Schema Documentation](graphql-schema.md)** - GraphQL queries, mutations, subscriptions
- **[Integration Guide](integration.md)** - Code examples in curl, Go, Python, JavaScript, gRPC

### Configuration & Deployment
- **[Configuration Reference](config.md)** - Complete aegion.yaml reference guide
- **[Architecture Guide](architecture.md)** - System design, data flow, components
- **[Security Guide](security.md)** - Authentication, authorization, encryption, compliance

### Operations & Maintenance
- **[Admin SPA User Guide](admin-spa.md)** - Dashboard, configuration UI, webhook management
- **[Webhook Integration Guide](webhooks.md)** - Event subscriptions, delivery, testing
- **[Performance Tuning](performance.md)** - Query optimization, caching, benchmarking
- **[Troubleshooting Guide](troubleshooting.md)** - Common issues and solutions
- **[FAQ](faq.md)** - Frequently asked questions and best practices

### Advanced Topics
- **[Migration Guide](migration.md)** - Migrating from existing analytics systems
- **[Performance Benchmarking](performance.md)** - Load testing and optimization

---

## 🚀 Quick Links

### For New Users
1. Start with [Quick Start Guide](quickstart.md) to set up locally
2. Read [Architecture Guide](architecture.md) to understand the system
3. Try the examples in [Integration Guide](integration.md)

### For Operators
1. Follow [Setup & Deployment Guide](setup.md) for production deployment
2. Review [Configuration Reference](config.md) for all settings
3. Check [Troubleshooting Guide](troubleshooting.md) for common issues
4. Use [Admin SPA User Guide](admin-spa.md) to manage the system

### For Integrators
1. Review [API Usage Guide](api.md) for REST API patterns
2. Check [GraphQL Schema Documentation](graphql-schema.md) for GraphQL
3. See [Integration Guide](integration.md) for code examples
4. Implement [Webhook Integration Guide](webhooks.md) for event subscriptions

### For Platform Teams
1. Review [Security Guide](security.md) for compliance requirements
2. Check [Performance Tuning](performance.md) for optimization
3. Plan capacity using [FAQ](faq.md) best practices
4. Plan migrations using [Migration Guide](migration.md)

---

## 📊 System Overview

**Aegion Analytics** is a self-hosted, production-grade analytics system designed for:
- Real-time event ingestion and querying
- Multi-storage backend support (local, S3, Iceberg, Kubernetes)
- GraphQL, REST, and gRPC APIs
- Webhook-based event subscriptions
- Custom dashboard creation and sharing
- Comprehensive audit logging and compliance

### Key Components
- **DuckDB**: Fast columnar database for analytics queries
- **Data Sync Layer**: Real-time synchronization from PostgreSQL
- **REST API**: Standard HTTP endpoints with pagination and filtering
- **GraphQL API**: Full GraphQL schema with subscriptions
- **gRPC API**: High-performance bidirectional streaming
- **Webhook System**: Event-driven integrations with retry logic
- **Admin SPA**: Web-based dashboard and configuration UI
- **Retention Engine**: Automatic data lifecycle management

### Storage Options
- **Local**: Single-file DuckDB for development
- **S3**: Object storage for scalable deployments
- **Iceberg**: Apache Iceberg format for data lakes
- **Kubernetes**: Distributed storage across cluster nodes

---

## 🔧 Configuration

The system is configured via a single `aegion.yaml` file with support for:
- Environment variable overrides
- Secret injection from files
- Profile-based configurations (dev, staging, production)
- Hot-reloadable settings

See [Configuration Reference](config.md) for complete details.

---

## 🔐 Security

Aegion Analytics includes:
- **Authentication**: Bearer token, API keys, JWT support
- **Authorization**: Role-based access control (RBAC)
- **Encryption**: AES-256 for data at rest, TLS for in-transit
- **Audit Logging**: Complete audit trail of all operations
- **Rate Limiting**: Per-user and per-endpoint limits
- **SQL Injection Prevention**: Parameterized queries throughout

See [Security Guide](security.md) for comprehensive security documentation.

---

## 📈 Performance

Key performance metrics:
- **Query Latency**: < 100ms for most queries (< 1s for large aggregations)
- **Throughput**: 100K+ events/sec ingestion
- **Storage**: Compression ratio ~10:1 with Parquet format
- **Retention**: Configurable policies with automatic purging

See [Performance Tuning](performance.md) for optimization guidance.

---

## 🔄 Webhook Integration

Real-time event subscriptions with:
- Event filtering and pattern matching
- HMAC-SHA256 signature verification
- Automatic retry with exponential backoff
- Dead letter queue for failed deliveries
- Full delivery history and debugging

See [Webhook Integration Guide](webhooks.md) for integration details.

---

## 📱 Admin Dashboard

The Admin SPA provides:
- 5 pre-built dashboards (authentication, user activity, sessions, security, system health)
- Custom dashboard builder with drag-and-drop components
- Webhook management and testing
- Query builder and execution
- Event viewer with filtering and export

See [Admin SPA User Guide](admin-spa.md) for UI documentation.

---

## 🆘 Support Resources

- **Issues**: Report bugs at https://github.com/NeerajCodz/Aegion/issues
- **Discussions**: Join discussions at https://github.com/NeerajCodz/Aegion/discussions
- **Documentation**: Full docs in this directory
- **Troubleshooting**: See [Troubleshooting Guide](troubleshooting.md)

---

## 📝 Documentation Structure

Each guide follows a consistent structure:
- **Overview** - What the guide covers
- **Concepts** - Key terminology and ideas
- **Step-by-Step** - Hands-on instructions
- **Examples** - Copy-paste ready code samples
- **Configuration** - YAML configuration snippets
- **Troubleshooting** - Common issues and solutions
- **Next Steps** - Related documentation and resources

---

## 🤝 Contributing to Documentation

If you find issues or want to improve documentation:
1. Fork the repository
2. Create a branch: `git checkout -b docs/improvement`
3. Make changes following the style guide above
4. Submit a pull request

---

## 📋 Version Information

- **Analytics Module**: Phase 11
- **Aegion Core**: Latest
- **Documentation Version**: 1.0
- **Last Updated**: 2026-04-23

For version-specific features, see release notes in the main README.

---

## 📖 Documentation Standards

All documentation in this directory follows these standards:
- ✅ Clear explanations without jargon (or definitions provided)
- ✅ Copy-paste ready code examples for all key concepts
- ✅ curl examples for all REST endpoints
- ✅ Python/Go/JavaScript examples for common tasks
- ✅ Configuration snippets from aegion.yaml
- ✅ Cross-references to related documentation
- ✅ Tables of contents for longer documents
- ✅ ASCII diagrams where helpful
- ✅ External resource links
- ✅ Version compatibility notes
- ✅ Security implications called out

---

## 🎯 What's Next?

Choose your path based on your role:

- **👨‍💻 Developers**: Start with [Quick Start Guide](quickstart.md) → [Integration Guide](integration.md)
- **🏗️ DevOps/SRE**: Start with [Setup & Deployment Guide](setup.md) → [Configuration Reference](config.md)
- **🔒 Security Teams**: Start with [Security Guide](security.md) → [Troubleshooting Guide](troubleshooting.md)
- **📊 Data Teams**: Start with [API Usage Guide](api.md) → [Performance Tuning](performance.md)

---

**Happy analyzing! 🎉**
