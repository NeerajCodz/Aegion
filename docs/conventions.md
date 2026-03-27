Here’s your content rewritten as a **clean, production-grade developer Markdown document**, generalized (not tied to BlankSage) and structured for reuse across projects.

---

# Branch Naming & Commit Conventions

This document defines **branch naming**, **commit message**, and **pull request (PR)** conventions for a production-grade backend service. These conventions ensure consistency, traceability, and enforceable quality gates across teams.

---

## Table of Contents

* [Branch Naming](#branch-naming)
* [Commit Messages](#commit-messages)
* [Pull Request Requirements](#pull-request-requirements)
* [Pre-commit Hooks](#pre-commit-hooks)
* [Branch Protection Rules](#branch-protection-rules)
* [Code Style](#code-style)
* [API Documentation (Swagger / OpenAPI)](#api-documentation-swagger--openapi)
* [Error Codes](#error-codes)
* [Logging](#logging)
* [Workflow Summary](#workflow-summary)

---

## Branch Naming

### Pattern

```
<type>/<TICKET>-<description>
```

### Components

* `<type>`: One of:

  * `feat` – new feature
  * `fix` – bug fix
  * `docs` – documentation
  * `test` – tests
  * `refactor` – code changes without behavior change
  * `chore` – maintenance (deps, tooling)
  * `ci` – CI/CD changes

* `<TICKET>`:

  * Issue tracker ID (e.g., `PROJ-123`)
  * Must be uppercase

* `<description>`:

  * Short, hyphenated, lowercase summary

---

### Examples

```bash
feat/PROJ-123-add-user-endpoint
fix/PROJ-456-handle-null-response
docs/PROJ-789-update-architecture
refactor/PROJ-321-extract-service-layer
ci/PROJ-654-add-test-workflow
```

---

### Invalid Examples

```bash
feature/add-user              # missing ticket
PROJ-123-add-user            # missing type
feat-PROJ-123-add-user       # wrong separator
feat/add-user                # missing ticket
feat/proj-123-add-user       # ticket must be uppercase
```

---

### Enforcement

* CI validates branch names on push
* Invalid branch names fail pipeline checks

---

## Commit Messages

### Pattern

```
<type>: <description>
```

### Rules

* `<type>` must match branch type
* Description:

  * Imperative mood (e.g., “add”, not “added”)
  * No trailing period
  * Keep concise (~50 chars recommended)

---

### Examples

```bash
feat: add user provisioning endpoint
fix: handle nil pointer in auth middleware
docs: update API usage examples
test: add coverage for login handler
refactor: simplify service layer dependencies
chore: upgrade Go version
ci: add lint step to pipeline
```

---

### Invalid Examples

```bash
Added user endpoint           # missing type
feat added endpoint           # missing colon
FEAT: add endpoint            # uppercase type
feat: Added endpoint.         # not imperative, has period
```

---

### Enforcement

* Commit messages validated via commit-msg hooks
* CI may enforce format on push

---

## Pull Request Requirements

All PRs must:

1. Include ticket reference in description
2. Pass all CI checks:

   * Branch validation
   * Lint
   * Tests
   * Build
3. Maintain required test coverage (e.g., ≥90% on new code)
4. Follow PR template checklist
5. Include API documentation updates if applicable
6. Include tests for new functionality
7. Validate database migrations (if any)

---

### PR Title Format

Same as commit format:

```
feat: add user provisioning endpoint
fix: resolve token validation issue
docs: update getting started guide
```

---

## Pre-commit Hooks

Pre-commit hooks enforce quality before code is committed.

### What They Check

* Linting (e.g., `golangci-lint`)
* Formatting (trailing whitespace, EOF newline)
* YAML / JSON validation
* Large file detection
* Merge conflict markers
* Private key leaks
* Commit message format

---

### Installation

```bash
# macOS
brew install pre-commit

# Recommended (cross-platform)
pipx install pre-commit

# Alternative
pip install --user pre-commit
```

---

### Setup

```bash
make precommit
```

---

### Usage

Hooks run automatically:

```bash
git add .
git commit -m "feat: add user endpoint"
```

If checks fail, commit is rejected.

---

### Emergency Bypass (Use Sparingly)

```bash
git commit --no-verify -m "fix: critical production issue"
```

> CI will still enforce checks on push.

---

## Branch Protection Rules

Configure repository settings:

* Require pull requests before merging
* Require at least one approval
* Require passing status checks:

  * Branch validation
  * Lint
  * Tests
  * Build
* Require branches to be up to date
* Disallow direct pushes to `main`

---

## Code Style

* Format using:

  * `gofmt`
  * `goimports`
* Lint using `golangci-lint`
* All exported functions/types must have comments
* JSON tags use `snake_case`
* Use validation tags (e.g., `validate:"required,min=1"`)
* Keep package names concise and lowercase

---

## API Documentation (Swagger / OpenAPI)

All HTTP endpoints must include OpenAPI annotations.

---

### When to Update

Run documentation generation when:

* Adding new endpoints
* Modifying request/response schemas
* Changing routes or methods
* Updating error responses

---

### Example Annotation

```go
// handleCreateUser godoc
//
// @Summary     Create a new user
// @Description Creates a user in the system
// @Tags        users
// @Accept      json
// @Produce     json
// @Param       body body models.CreateUserRequest true "User payload"
// @Success     201 {object} models.UserResponse
// @Failure     422 {object} models.ErrorResponse
// @Router      /api/v1/users [post]
func handleCreateUser(c echo.Context) error {
    // implementation
}
```

---

### Rules

* Use named functions only
* Reference shared `models` package
* Commit generated docs to repository
* Serve Swagger UI in all environments

---

## Error Codes

Use structured error responses with standardized codes.

### Format

```json
{
  "code": "VALIDATION_ERROR",
  "message": "Invalid input",
  "trace_id": "abc123"
}
```

---

### Common Codes

| Code             | HTTP Status | Description             |
| ---------------- | ----------- | ----------------------- |
| VALIDATION_ERROR | 422         | Input validation failed |
| INTERNAL_ERROR   | 500         | Unexpected server error |
| UNAUTHORIZED     | 401         | Authentication failure  |
| FORBIDDEN        | 403         | Permission denied       |

---

## Logging

Use structured logging only.

---

### Rules

* Use structured logger (e.g., `slog`)
* Always include context
* Never log sensitive data
* Avoid unstructured logging (`fmt.Println`, `log.Println`)

---

### Example

```go
slog.InfoContext(ctx, "user created",
    "user_id", user.ID,
    "email", user.Email,
)
```

---

### Automatically Injected Fields

* `service_name`
* `environment`
* `request_id`
* `trace_id`
* `span_id`

---

## Workflow Summary

```bash
# 1. Create branch
git checkout -b feat/PROJ-123-add-user-endpoint

# 2. Make changes and commit
git add .
git commit -m "feat: add user endpoint"

# 3. Push branch
git push origin feat/PROJ-123-add-user-endpoint

# 4. Open PR
# - Fill checklist
# - Link ticket
# - Ensure CI passes

# 5. Merge after approval
# - Prefer squash merge
# - Delete branch
```

---

## Key Principles

* **Consistency over flexibility**
* **Automation over manual enforcement**
* **Traceability via tickets and commits**
* **Quality gates before merge**
* **Documentation as part of development**

---

If you want next step, I can:

* Add **GitHub Actions config** for enforcing these rules
* Provide a **`.pre-commit-config.yaml` ready to use**
* Or tailor this specifically for your **SCIM/auth platform repo**