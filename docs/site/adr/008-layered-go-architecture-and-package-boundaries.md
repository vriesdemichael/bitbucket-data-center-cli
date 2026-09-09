---
search:
  boost: 0.3
---

# ADR 008: Layered Go architecture and package boundaries

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `008`
- Title: `Layered Go architecture and package boundaries`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/008-layered-go-architecture-and-package-boundaries.yaml`

## Decision

Implement the system in four layers with strict dependency direction: transport -> api services -> workflows -> cli. Cross-layer shortcuts are disallowed unless explicitly approved in a superseding decision.

## Agent Instructions

Place new code in the narrowest responsible layer. Keep transport concerns out of workflows and CLI concerns out of service packages. When a change seems to require cross-layer coupling, propose a design adjustment first.

## Rationale

Strong boundaries reduce accidental complexity and make Bitbucket behavior handling testable. This structure keeps a stable public interface while isolating server quirks in controlled locations.

## Rejected Alternatives

- `Single service package with mixed responsibilities`: Leads to hard-to-test coupling between HTTP, mapping, and workflow behavior.
