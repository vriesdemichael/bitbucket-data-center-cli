---
search:
  boost: 0.3
---

# ADR-014: Output contract for human and machine modes

> Replaced by [ADR-064](064-machine-output-carries-no-contract-version.md).

Provide consistent human-readable output by default and a stable machine mode via --json. JSON responses must use versioned envelopes for forward-compatible parsing.

Keep display formatting separated from domain/workflow logic. Ensure every data-returning command supports --json and stable field naming. Changes to JSON contracts require explicit versioning and migration notes.

The CLI must serve both interactive operators and automation scripts without ambiguity. Stable machine contracts reduce breaking changes and improve integration reliability.

## Not chosen

- **Human output only**: Not sufficient for automation and CI/local scripting workflows.
- **Unversioned JSON payloads**: Harder to evolve safely over time.
