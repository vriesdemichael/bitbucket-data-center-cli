---
search:
  boost: 0.3
---

# ADR 003: AI agents as first-class developers

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `003`
- Title: `AI agents as first-class developers`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/003-ai-agents-as-first-class-developers.yaml`

## Decision

Development-critical knowledge must be stored in AI-discoverable locations such as AGENTS.md, decision records, architecture docs, and focused inline rationale comments. AI agents are treated as first-class contributors and must be able to onboard from repository artifacts without tribal knowledge.

## Agent Instructions

Always document important standards and assumptions in discoverable project files.
Do not rely on implicit conventions. When you find recurring but undocumented patterns,
propose adding or updating a decision record or AGENTS.md guidance.

ADR content placement:
- Use `agent_instructions` for information an agent must know before modifying areas.
- Use `decision` and `rationale` for architectural context and trade-offs.
- Keep instructions concise and actionable to reduce context noise.

## Rationale

High-quality AI collaboration requires the same contextual access humans need. Explicit, structured documentation reduces ambiguity, speeds onboarding, and keeps implementation decisions consistent across sessions and contributors.

## Rejected Alternatives

- `Depend mainly on code comments for project conventions`: Comments are distributed and rarely capture cross-cutting workflow or architecture policy.
- `Assume agents infer conventions from existing code`: Inference is slower, inconsistent, and increases drift in implementation style.
