---
search:
  boost: 0.3
---

# ADR-028: A live test pins what Bitbucket does, and fixes.yaml records where bb works around its specification

Each undocumented or surprising thing Bitbucket does is recorded as a live test that asserts it, named for what the server does, with a comment saying why it matters to bb. Behaviour derived from the published specification is trusted only once a live test seeds the entities and reads them back with real, non-empty data; a parity test that lists nothing proves no contract.

docs/openapi/fixes.yaml records every place bb works around the published specification, whether the document is wrong about the release it describes or the generator cannot use it as written, and what bb does about it: a sanitizer rule, a fix to the generated code, an adapter or a test. Each entry carries a description, a reference, the change and the files it touched, and commands that verify it. A capability an older release lacks is not a specification error: it is a difference between releases, catalogued in docs/site/reference/bitbucket-versions.md (ADR-088).

When you find a quirk, add or update a targeted live test in the same change as the code that depends on it. Do not keep it in memory, in a note, or in a mock. Update the registry in the same change as any fix to the specification, the generated code, an adapter or a test that works around one. When upstream or the generator removes the need for a fix, remove or update its entry and include the evidence in the change.

A test keeps the knowledge true: when a Bitbucket release changes the behaviour, the test fails, where a note would go on describing the old one. Without the registry, why a sanitizer or an adapter exists becomes tribal knowledge, and a regression after a refresh is hard to place.
