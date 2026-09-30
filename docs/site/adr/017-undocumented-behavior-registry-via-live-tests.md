---
search:
  boost: 0.3
---

# ADR-017: Undocumented behavior registry via live tests

Treat undocumented or surprising Bitbucket behavior as first-class compatibility knowledge by encoding each finding as an explicit live test with a descriptive name and rationale.

When discovering quirks, add or update a targeted live test and include a concise explanation in test naming or adjacent documentation. Do not rely on memory or ad-hoc notes for behavior exceptions.

Executable behavior knowledge prevents regressions and creates durable project memory. It is especially important for APIs with inconsistent documentation quality.

## Not chosen

- **Track quirks only in prose docs**: Not enforceable and prone to drift.
