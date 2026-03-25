# Aegion — Policy Engine Specification

Aegion policy (`aegion-policy`) combines RBAC, ABAC, and ReBAC in one system. It runs as a separate independently scalable container, called synchronously via gRPC by any module needing an authorization decision.

---

## Why this module exists

Authentication answers **who** the subject is. Policy answers **what** the subject can do.

Aegion policy is designed to handle:
- Simple role systems (admin/editor/viewer)
- Context-aware business rules (owner can edit if status = draft)
- Enterprise graph access (team/folder/project inheritance)
- All three simultaneously, with explicit, deterministic precedence between models

---

## How policy is called

```
aegion-proxy  ──gRPC: PolicyEngine.Check──────────► aegion-policy
aegion-oauth2 ──gRPC: PolicyEngine.Check──────────► aegion-policy
Your app      ──POST /relation-tuples/check────────► aegion-policy (via core ingress)
aegion-admin  ──gRPC: PolicyEngine.{role,rule,tuple operations}──► aegion-policy
```

Batch checks are available via `PolicyEngine.BatchCheck` for scenarios where a proxy rule needs to evaluate multiple resources simultaneously.

The full gRPC interface is defined in `proto/policy/policy.proto` (see `inter-module-communication.md`).

---

## RBAC

RBAC maps identities to roles and roles to permissions.

### Data model

```
Permission:   (resource_type TEXT, action TEXT)
              e.g. ("posts", "write") or ("*", "*")

Role:         name TEXT, permissions[] Permission

Assignment:   identity_id → role_id
              (stored in pol_role_assignments)
```

### Wildcard support

Resource type and action both support the `*` wildcard:
- `("*", "*")` — grants all actions on all resource types (super-admin pattern)
- `("posts", "*")` — grants all actions on `posts` resource type
- `("*", "read")` — grants read on all resource types

Wildcards are resolved at check time, not at assignment time. Evaluation order for wildcards: exact match first, then wildcard resource type, then wildcard action, then `*.*`.

### Evaluation

```
Input: subject=user:alice, resource_type=posts, action=write

1. Fetch role assignments for identity alice from pol_role_assignments
2. Fetch permissions for each role from pol_permissions
3. Check if any permission matches (resource_type, action):
   - Exact: ("posts", "write") ✓
   - Resource wildcard: ("posts", "*") ✓
   - Action wildcard: ("*", "write") ✓
   - Full wildcard: ("*", "*") ✓
4. If any match: RBAC allow
5. If no match: no RBAC allow (fall through to ABAC)
```

RBAC lookups are cached in-process with a short TTL (configurable, default 30s). Cache is invalidated on `identity.updated` and `policy.rule_changed` events from the event bus.

---

## ABAC

ABAC evaluates CEL rule expressions against subject, resource, and request context.

### CEL evaluation environment

The following variables are available in every CEL expression:

```
subject
  .id          string    identity UUID
  .roles       []string  role names assigned to this identity
  .traits      map       identity traits from core_identities.traits
  .metadata    map       identity metadata_public from core_identities
  .aal         string    "aal1" | "aal2" — from the current session

resource
  .id          string    resource identifier (from the check request)
  .type        string    resource type (from the check request)
  .owner_id    string    resolved from context.extra["owner_id"] if provided
  .metadata    map       resolved from context.extra["resource_metadata"] if provided

action         string    the action being checked

request.context
  .ip          string    client IP address
  .time        google.protobuf.Timestamp
  .tenant_id   string
  .extra       map       any additional context fields passed in CheckRequest.Context.extra
```

Callers enrich the context by passing additional fields in `CheckRequest.Context.extra`. For example, an application checking whether a user can edit a document passes `extra["owner_id"] = document.owner_id` and `extra["document_status"] = "draft"`. The CEL rule can then reference `resource.owner_id` and `request.context.extra["document_status"]`.

### CEL rule examples

```cel
// Owner can edit their own draft documents
subject.id == resource.owner_id && request.context.extra["document_status"] == "draft"

// Admin bypass
subject.roles.exists(r, r == "admin")

// Business hours only (UTC)
request.context.time.getHours() >= 9 && request.context.time.getHours() < 18

// IP allowlist for sensitive operations
request.context.ip.startsWith("10.0.") || request.context.ip.startsWith("172.16.")

// Tenant isolation
subject.traits["tenant_id"] == resource.metadata["tenant_id"]
```

### Explicit DENY rules

ABAC rules can have `effect: deny`. A DENY rule with higher priority than any ALLOW rule runs first and immediately returns a deny without evaluating subsequent rules. This is the explicit override mechanism:

```
priority 1  effect: deny   expression: request.context.ip in BLOCKLIST
priority 10 effect: allow  expression: subject.roles.exists(r, r == "admin")

Result for admin from blocked IP: DENY (rule 1 fires before rule 10)
```

---

## ReBAC

ReBAC uses a Zanzibar-style tuple graph for relationship-based access.

### Tuple model

```
(namespace, object_id, relation, subject_id)

Examples:
  ("files",  "doc:spec",    "owner",   "user:alice")
  ("files",  "doc:spec",    "viewer",  "group:eng#member")
  ("groups", "group:eng",   "member",  "user:bob")
  ("files",  "folder:designs", "viewer", "group:design")
  ("files",  "doc:spec",    "parent",  "folder:designs")
```

The `#member` suffix in `group:eng#member` is a subject-set expression meaning "any identity that has the `member` relation to `group:eng`". This is how group-based access is expressed without denormalized per-user tuples.

### Namespace definition

A namespace defines the object types and their relations. It is defined in OPL (Object Permission Language) format stored in `pol_rebac_namespaces.config`:

```json
{
  "name": "files",
  "relations": {
    "owner": {
      "types": ["user"]
    },
    "editor": {
      "types": ["user", "group#member"],
      "inherits": ["viewer"]
    },
    "viewer": {
      "types": ["user", "group#member"],
      "inherits": []
    },
    "parent": {
      "types": ["folder"],
      "inherits_from_parent": ["viewer", "editor"]
    }
  }
}
```

The `inherits` field means: a subject with the `editor` relation implicitly also has the `viewer` relation. The `inherits_from_parent` field means: the `parent` relation causes the object to inherit the `viewer` and `editor` relations from the parent folder.

### Namespace versioning and activation

1. A new namespace config is written with `active: false` via the admin panel.
2. The config is validated (no circular inheritance, all referenced types exist).
3. Validation failures are returned immediately — the namespace is never saved in invalid state.
4. Operator reviews the config in the admin panel simulation tool.
5. Operator activates the namespace: `active` is set to `true`, `version` incremented.
6. `policy.namespace_activated` event is published.
7. The `aegion-policy` container flushes its expansion cache for this namespace.
8. Old namespace version is kept for 24h for graceful rollover; then soft-deleted.

A namespace cannot be deactivated while relation tuples referencing it exist. The admin panel enforces this and shows the tuple count before allowing deactivation.

### ReBAC expansion algorithm

The expansion algorithm used by the Rust policy engine is iterative (not recursive) to avoid stack overflows on deep graphs:

```
Check: can user:alice perform action "view" on file:doc?

1. Map action "view" to relation "viewer" (via namespace config)

2. Initialize work queue: Q = [{ object: "file:doc", relation: "viewer" }]
   Initialize visited set: V = {}

3. While Q is not empty:
   a. Dequeue { object, relation }
   b. If (object, relation) in V: skip (cycle detected)
   c. Add (object, relation) to V

   d. Query pol_rebac_tuples WHERE namespace="files"
        AND object_id=object AND relation=relation
      Returns tuples like:
        subject_id = "user:alice"           → DIRECT MATCH → return ALLOW
        subject_id = "group:eng#member"     → subject-set: enqueue check for
                                              (group:eng, member, user:alice)
        subject_id = "folder:designs#viewer" → parent expansion: enqueue
                                              check via parent relation

   e. For each subject-set tuple: enqueue (object=subject_set_object,
                                           relation=subject_set_relation)

   f. For inherits relations in namespace config: for each relation R that
      inherits from current relation, enqueue (object, R)

   g. For parent relations: fetch parent tuples and enqueue parent checks

4. If Q exhausted without finding user:alice: return DENY

5. Cache result: (namespace, object, relation, subject) → allow/deny
   with TTL = expansion_cache_ttl (default 60s)
```

The `max_depth` config (default 20) limits the number of work queue iterations. If max_depth is exceeded, the check returns DENY with `deny_reason: "max_depth_exceeded"` — this prevents runaway traversal on malformed or adversarial tuple graphs.

---

## Evaluation strategy and precedence contract

The evaluation order is a **hard implementation contract**:

```
1. ABAC DENY rules (effect: deny, evaluated in priority order)
   → If any deny rule matches: return DENY immediately, no further evaluation

2. RBAC ALLOW
   → Check role assignments and permissions
   → If any permission matches: return ALLOW

3. ABAC ALLOW rules (effect: allow, evaluated in priority order)
   → Evaluate CEL expressions against subject/resource/context
   → If any allow rule matches: return ALLOW

4. ReBAC ALLOW
   → Expand tuple graph for (namespace, object, relation, subject)
   → If expansion finds subject: return ALLOW

5. DEFAULT DENY
   → No rule granted access: return DENY
```

Why this order:
- DENY rules always win — no amount of role or relationship can override an explicit deny
- RBAC runs before ABAC — simpler DB lookups execute first; CEL evaluation only runs if RBAC doesn't resolve it
- ReBAC runs last — it is the most expensive evaluation (graph traversal, potentially many DB queries)
- Default deny — no assumption of access on missing data

This is the contract. Any implementation that deviates is a correctness bug.

---

## Performance model

### In-process caches (in aegion-policy container)

```
RBAC cache:        LRU, identity_id → roles + permissions, TTL 30s
ABAC rule cache:   Full rule set loaded at startup, refreshed on policy.rule_changed event
ReBAC expansion:   LRU, (namespace, object, relation, subject) → allow/deny, TTL 60s
Namespace config:  In-memory, refreshed on policy.namespace_activated event
```

All caches are invalidated by event bus events. The event bus subscription is a gRPC stream from core — `aegion-policy` subscribes on startup and receives invalidation events in near real-time (within 1 second of a rule change).

### Rust ReBAC engine

The tuple graph traversal is executed in Rust (called via core's internal gRPC). The Rust engine maintains its own in-process tuple cache separate from the Go-layer expansion cache. The two-layer cache architecture:

```
aegion-policy (Go layer)
    expansion cache:  final allow/deny result per (namespace, object, relation, subject)
         ↓ cache miss
    Rust engine (via core gRPC)
         tuple cache: raw tuples per (namespace, object_id, relation)
              ↓ cache miss
         Postgres pol_rebac_tuples
```

The tuple cache in Rust is populated during traversal. Subsequent checks that traverse overlapping paths reuse cached tuples.

---

## Event bus subscriptions

`aegion-policy` subscribes to:

| Event | Action |
|---|---|
| `identity.updated` | Flush RBAC assignment cache for that identity |
| `identity.state_changed` | Flush all caches for that identity |
| `identity.deleted` | Flush all caches for that identity; clean up role assignments and tuples |
| `policy.rule_changed` | Flush ABAC rule cache (full reload); flush affected ReBAC expansion cache |
| `policy.namespace_activated` | Reload namespace config; flush ReBAC expansion cache for that namespace |
| `scim.user_provisioned` | If initial role assignment is configured for the connection, assign roles |
| `key.rotated` | Re-validate internal auth token |

---

## Scaling aegion-policy

`aegion-policy` is stateless except for its in-process caches. All instances connect to the same Postgres database. Cache invalidation events are delivered to all instances via the event bus (each instance maintains its own gRPC subscription stream to core).

For high-volume authorization workloads, scale `aegion-policy` independently:

```bash
kubectl scale deployment aegion-policy --replicas=8
```

Core's service registry load-balances policy checks across all healthy instances using least-connections. For ReBAC-heavy workloads, the Rust expansion engine is the bottleneck — more replicas = more parallel graph traversal workers.

---

## Admin operations

Policy admin surface (via `aegion-admin`):

- **Roles**: CRUD, permission assignment per role, bulk assign/revoke
- **Role assignments**: assign roles to identities, view effective permissions per identity
- **ABAC rules**: CRUD, priority ordering, live enable/disable without restart, CEL expression syntax validation
- **Namespaces**: config management, validation before activation, version history
- **Relation tuples**: view, search, create, delete, bulk import (CSV/JSON), bulk delete by namespace
- **Policy simulation**: evaluate a check request with full trace output — shows which model resolved it and why
- **Staged rollout**: mark a rule as "shadow mode" (logged but not enforced) before activating it

---

## Security and governance

- Default deny when no rule grants access — never assume allow on missing data
- Full audit records for all policy mutations (via `admin.action` event and `core_audit_events`)
- Namespace/rule activation requires explicit validation — invalid configs are never persisted
- `aegion-policy` connects to Postgres with `aegion_policy` role: write access only to `pol_*` tables, read-only access to `core_identities`
- CEL expressions are compiled and validated at write time — a syntactically invalid rule is rejected, never stored
- ReBAC `max_depth` protects against malformed or adversarial tuple graphs
