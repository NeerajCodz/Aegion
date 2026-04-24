# Aegion Analytics Docs

This directory currently contains the analytics execution tracker and the minimum verified operator docs that exist on the `beta` branch.

## Available now

- [Master plan](plan.md) - code-verified implementation tracker for analytics work
- [QA runbook](qa.md) - regression and smoke-check checklist
- [Quickstart](quickstart.md) - local setup notes constrained to docs that actually exist

## Pending docs

The following documents are planned but are **not** present yet on this branch. Their delivery is tracked in [plan.md](plan.md).

- OpenAPI spec
- GraphQL schema guide
- Configuration reference
- REST API guide
- Architecture guide
- Setup guide
- Integration guide
- Admin SPA guide
- Performance guide
- Webhook guide
- Security guide
- Troubleshooting guide
- FAQ

## How to use this folder

1. Start with [plan.md](plan.md) to see what is shipped versus partial.
2. Use [qa.md](qa.md) for validation commands and manual smoke checks.
3. Use [quickstart.md](quickstart.md) only as a beta-branch bootstrap reference, not as a complete product guide.

## Source of truth

- Code status comes from `modules/analytics`, `modules/admin/spa`, `configs/aegion.yaml`, and recent `git log`.
- If a doc claim conflicts with the code, the code wins and the docs should be corrected in the same tranche.
