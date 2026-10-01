---
search:
  boost: 0.3
---

# ADR-010: Go model validation and schema export

Inputs and configuration are typed Go structs with explicit validation rules, checked at the boundaries: configuration load, request payload construction and external input parsing. A schema is derived from the code that owns the shape, never kept as a hand-maintained file beside it.

Every command declares the result type it fills in `internal/cli/result`, or states in `internal/cli/outputschemas` why it has none; `TestEveryCommandIsModelled` fails on a command that does neither. `--describe` gives the JSON Schema of the whole document a command writes, for a run and for a dry run (ADR-097), derived from that type when it is asked for rather than kept as a file. `docs/reference/schemas` exports the schemas read outside bb: the failure envelope, which every command shares (ADR-046), and the configuration schema, which editors and CI validate against. `docs:verify-generated` keeps each export in step with its source.

A schema file is a copy of a Go type, and a copy is where the two stop agreeing. Deriving it gives strict validation and an explicit contract that cannot drift from what the binary does.

## Not chosen

- **Validation only in business logic**: Error-prone and inconsistent, with weaker contract guarantees.
- **Handwritten schemas detached from model types**: High drift risk and duplicate maintenance burden.
