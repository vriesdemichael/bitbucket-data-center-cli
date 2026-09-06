# ADR 010: Go model validation and schema export

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `010`
- Title: `Go model validation and schema export`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/010-go-model-validation-and-schema-export.yaml`

## Decision

Use typed Go structs with explicit runtime validation rules for inputs and configuration, and support JSON schema export for machine validation of JSON/YAML examples and docs.

## Agent Instructions

Define validation close to model definitions and validate at boundaries (config load, request payload construction, and external input parsing). Keep schemas generated from the model source of truth rather than hand-maintained files. A command output schema is served on demand by `--describe`, derived from the handler type at the moment it is asked for, and is not published as a file on disk. The 200-odd committed per-command schemas this record once implied were deleted with #485: a schema file is a copy of a Go type, and a copy is where the two stop agreeing. Configuration and bulk workflow schemas are still exported, because their consumers are editors and CI rather than bb itself.

## Rationale

This preserves core benefits previously achieved with Pydantic: strict data validation, explicit contracts, and schema-driven validation for documentation examples.

## Rejected Alternatives

- `Validation only in business logic`: Error-prone and inconsistent, with weaker contract guarantees.
- `Handwritten schemas detached from model types`: High drift risk and duplicate maintenance burden.
