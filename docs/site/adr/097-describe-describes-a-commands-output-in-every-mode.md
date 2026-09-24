---
search:
  boost: 0.3
---

# ADR 097: --describe describes a command's output in every mode

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `097`
- Title: `--describe describes a command's output in every mode`
- Category: `architecture`
- Status: `accepted`
- Amends: `010`
- Provenance: `guided-ai`
- Source: `docs/decisions/097-describe-describes-a-commands-output-in-every-mode.yaml`

## Decision

--describe answers what a command returns, offline and whatever other flags are given, in the description member (ADR-096). For a command it holds the JSON Schema of the whole document for a run and for a dry run, each as outputSchema, with what --dry-run does for the command and at which tier. For a group or the root it lists the commands and what --dry-run does for each, so one call covers the whole tool. A path that names no command gets its error inside description, and exits 2 in text. In text it prints the same for people: an outline of the document's fields, with their types, allowed values and descriptions, marking those that can be absent. The help line for --describe says it describes the output.

## Agent Instructions

Derive the schemas, the outline and the dry-run behaviour from the command's result type and its dry-run classification; never write them by hand. Render every outline from its schema with one renderer. Do not add arguments or flags to the description: --help covers input.

## Rationale

A caller needs the shape of the output before it runs anything, and that is the part --help does not give. A schema of data alone cannot validate a document, and says nothing about what --dry-run returns. The outline serves a person, and an agent that only needs the shape, at a fraction of the schema's length.

## Rejected Alternatives

- `Arguments and flags in the description`: --help already gives them, and each argument would need a description written by hand.
- `The schema as bare JSON when neither --json nor --yaml is given`: JSON without --json, and not the envelope either; a person gets a wall of schema and a validator a wrapper.
- `A new flag name such as --output-schema`: A deprecation cycle for a flag explained wherever it appears; the key and the help line say output.
