---
search:
  boost: 0.3
---

# ADR-015: Live test harness and deterministic seeding

Build a deterministic live integration harness using the local Bitbucket stack, seeded baseline entities, unique per-test namespaces where feasible, and repeatable cleanup/reset workflows.

Live tests must avoid hidden ordering dependencies. Prefer unique test resources and explicit teardown. When adding new domains, extend seeding/reset logic to keep tests reproducible.

Deterministic setup is necessary for reliable live behavior validation and low maintenance overhead. It reduces flaky failures caused by shared mutable state.

## Not chosen

- **Shared long-lived mutable test state**: Increases flakiness and debugging cost.
