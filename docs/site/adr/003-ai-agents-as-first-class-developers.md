---
search:
  boost: 0.3
---

# ADR-003: AI agents as first-class developers

Agents are first-class contributors here, and everything a contributor needs to know is written down where an agent finds it: AGENTS.md for the mechanics and pitfalls of working in this repository, CONTRIBUTING.md for the process, the decision records for the rules and why they hold, the docs for how bb behaves, and a focused comment beside code whose reason the code does not show. Nothing depends on tribal knowledge or on a convention inferred from the code. When you find a recurring convention that is written nowhere, propose a record or an AGENTS.md entry for it. AGENTS.md repeats the rules from CONTRIBUTING.md that an agent must not lose, because an agent keeps AGENTS.md in context through compaction and drops a file it read once; change such a rule in both.

A decision record states a rule as it holds now, in prose: the rule first, then why it holds, with what a contributor or an agent must do in the same prose, and the alternatives turned down under Not chosen. CONTRIBUTING.md, under Writing a decision record, has the shape. Keep it short: an agent carries what it reads in its context, where a sentence with no rule, reason or instruction in it is noise. An agent reads a record as authoritative and cannot tell a stale one from a live one, so a record that stops holding is changed or deleted, never left standing.

An agent needs the same context a person does, and a session keeps none of it. Standards written down explicitly keep the next session, and the next contributor, consistent with the last.

## Not chosen

- **Conventions in code comments alone**: Comments are scattered, and rarely capture a rule that spans the repository.
- **Let agents infer conventions from the code**: Slower, inconsistent, and it copies whatever the code got wrong.
