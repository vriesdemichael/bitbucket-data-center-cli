---
search:
  boost: 0.3
---

# ADR-001: Go as primary implementation language

bb is written in Go and ships as one static binary per platform, and every tool under `tools/` is Go too. Python is confined to the documentation build and a few release steps, and nothing bb ships depends on it. Write new code in Go.

A static binary installs by copying one file, needs no runtime on the machine it runs on, and behaves the same on a workstation and in CI.

## Not chosen

- **Python as the implementation language**: A standalone install would need a runtime or a bundling step on every platform bb ships to.
