# Aegion Release Maturity Matrix

This table summarizes the current release stance for each module. The machine-checked source of truth is `build/release-maturity.json`.

## Maturity levels

- **GA**: supported for production with no known scope caveat.
- **Beta (deployable with caveats)**: production-capable for selected workloads; caveats must be accepted.
- **Not GA**: available for development and integration testing only.

## Module matrix

| Module | Maturity | Deployment mode | Notes |
|---|---|---|---|
| core | GA | hybrid control-plane | Primary ingress, orchestration, and shared auth/session runtime |
| password | GA | embedded in core | Stable first-factor flow in core orchestration path |
| magic_link | GA | embedded in core | Stable passwordless/recovery delivery path |
| policy | Beta | embedded at GA default | Extraction to standalone is optional post-GA |
| admin | Beta | standalone | Backend broad; SPA coverage and contract harmonization still closing |
| oauth2 | Beta | standalone | Protocol closure work remains (request context + subject strategy) |
| social | Beta | standalone | Provider/runtime completion still under hardening |
| sso | Beta | standalone | Enterprise validation and lifecycle hardening in progress |
| introspection | Beta | standalone | Token-state consistency follows OAuth2 closure work |
| proxy | Beta | standalone | Operator diagnostics/simulation/policy integration still maturing |
| mfa | Not GA | standalone | Present but not release-hardened |
| passkeys | Not GA | standalone | Present but not release-hardened |
| cli | Not GA | standalone | Internal/ops-facing surface not GA-hardened |

## Release gate intent

For GA and Beta modules, release approval requires passing baseline gates from CI/security, release checklist validation (`build/release-checklist.json`), and module-specific hardening requirements described in `build/release-maturity.json`.
