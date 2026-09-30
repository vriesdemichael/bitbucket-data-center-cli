---
search:
  boost: 0.3
---

# ADR-008: Layered Go architecture and package boundaries

Implement the system in four layers with strict dependency direction: transport -> api services -> workflows -> cli. Cross-layer shortcuts are disallowed unless explicitly approved in a superseding decision.

Place new code in the narrowest responsible layer. Keep transport concerns out of workflows and CLI concerns out of service packages. When a change seems to require cross-layer coupling, propose a design adjustment first.

Strong boundaries reduce accidental complexity and make Bitbucket behavior handling testable. This structure keeps a stable public interface while isolating server quirks in controlled locations.

## Not chosen

- **Single service package with mixed responsibilities**: Leads to hard-to-test coupling between HTTP, mapping, and workflow behavior.
