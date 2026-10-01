---
search:
  boost: 0.3
---

# ADR-028: OpenAPI fix registry and parity enforcement

docs/openapi/fixes.yaml records every place bb works around the published specification, whether the document is wrong about the release it describes or the generator cannot use it as written, and what bb does about it: a sanitizer rule, a fix to the generated code, an adapter or a test. Each entry carries a description, a reference, the change and the files it touched, and commands that verify it. A capability an older release lacks is not a specification error: it is a difference between releases, catalogued in docs/site/reference/bitbucket-versions.md (ADR-088). Behaviour derived from the specification is trusted only once a seeded live test exercises it with real, non-empty data.

Update the registry in the same change as any fix to the specification, the generated code, an adapter or a test that works around one. When upstream or the generator removes the need for a fix, remove or update its entry and include the evidence in the change. Do not rest contract confidence on a parity test that lists nothing; seed the entities and read them back.

Without a registry, why a sanitizer or an adapter exists becomes tribal knowledge, and a regression after a refresh is hard to place.

## Not chosen

- **Keep OpenAPI fixes only in commit history**: Hard to discover, weak for onboarding, and poor at enforcing ongoing hygiene.
- **Treat generated output as self-documenting**: Generated code shows final state but not why deviations or sanitizers were needed.
- **Use parity tests without data seeding**: Empty-state checks can pass while failing to prove real contract behavior.
