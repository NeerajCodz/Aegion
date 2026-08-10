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
| policy | GA | embedded in core | Fail-closed RBAC, ABAC, and ReBAC enforcement |
| admin | GA | external | Authenticated management and SCIM runtime through the core gateway |
| analytics | GA | external | Durable REST plus authenticated, bounded GraphQL |
| oauth2 | GA | external | OAuth2/OIDC runtime with gateway-owned public routes |
| social | GA | external | Durable provider flows with verified core identity context |
| sso | GA | external | Durable SAML connections and callback lifecycle |
| introspection | GA | external | Authenticated OAuth2 token introspection |
| proxy | GA | external | Identity-aware proxy control plane with constrained egress |
| mfa | GA | external | Durable multi-factor authentication lifecycle |
| passkeys | GA | external | WebAuthn credential and sign-counter enforcement |
| cli | GA | on-demand job | Operator artifact with explicit core API and mTLS credentials |

## Release gate intent

For GA and Beta modules, release approval requires passing baseline gates from CI/security, release checklist validation (`build/release-checklist.json`), and module-specific hardening requirements described in `build/release-maturity.json`.
