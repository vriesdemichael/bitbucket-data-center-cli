---
search:
  boost: 0.3
---

# ADR-017: Undocumented behavior registry via live tests

Each undocumented or surprising thing Bitbucket does is recorded as a live test that asserts it, named for what the server does, with a comment saying why it matters to bb. Where the published specification is wrong about it, docs/openapi/fixes.yaml records the correction as well (ADR-028).

When you find a quirk, add or update a targeted live test in the same change as the code that depends on it. Do not keep it in memory, in a note, or in a mock.

A test keeps the knowledge true. When a Bitbucket release changes the behaviour, the test fails, where a note would go on describing the old one. APIs whose documentation is uneven need this most.

## Not chosen

- **Track quirks only in prose docs**: Not enforceable, and prone to drift.
