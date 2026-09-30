---
search:
  boost: 0.3
---

# ADR-028: OpenAPI fix registry and parity enforcement

> Changed in part by [ADR-088](088-every-bitbucket-release-atlassian-supports-is-served.md).

Maintain a dedicated YAML registry at docs/openapi/fixes.yaml for every workaround, sanitizer rule, post-generation fix, or runtime/test adaptation needed on top of Atlassian's vendored OpenAPI artifact to produce usable generated models/endpoints. Each registry entry must include description, reference, detailed change notes, and executable verification commands. OpenAPI-derived behavior is considered trustworthy only when covered by seeded live parity tests that exercise non-empty flows.

Whenever you add or modify any OpenAPI-related fix (spec sanitation, generation patch, adapter behavior, or compatibility test), update docs/openapi/fixes.yaml in the same change. Do not rely on empty-list parity tests for contract confidence; prefer seeded live tests that create and verify real entities. If a fix is removed because upstream spec/tooling improved, remove or update the registry entry and include verification evidence in the change.

Generated code depends on both upstream spec quality and generator behavior. Without explicit fix logging, compatibility knowledge becomes tribal and regressions are hard to diagnose. A structured fix registry plus seeded parity tests creates an auditable trail and a repeatable trust model for future endpoint expansion.

## Not chosen

- **Keep OpenAPI fixes only in commit history**: Hard to discover, weak for onboarding, and poor at enforcing ongoing hygiene.
- **Treat generated output as self-documenting**: Generated code shows final state but not why deviations or sanitizers were needed.
- **Use parity tests without data seeding**: Empty-state checks can pass while failing to prove real contract behavior.
