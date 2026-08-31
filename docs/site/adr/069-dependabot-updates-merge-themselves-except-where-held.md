# ADR 069: Dependabot updates merge themselves, except where held for a person

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `069`
- Title: `Dependabot updates merge themselves, except where held for a person`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/069-dependabot-updates-merge-themselves-except-where-held.yaml`

## Decision

Dependabot proposes dependency updates and .github/workflows/dependabot-automerge.yml approves and rebase-merges them once CI is green. This is deliberate: a green run is accepted as sufficient evidence that an update is safe, without a human reading the diff. Three things are held for a person instead: major version bumps, any update whose type will not parse, and the Bitbucket product image whatever its update type, because the tag under test is a user-facing support claim (ADR-042). Ecosystems, schedules, grouping and the held set live in .github/dependabot.yml and the workflow. This record does not restate them.

## Agent Instructions

Do not add a dependency exception to this record; add it to the workflow, where it is executable. Do not weaken a CI gate to get an update through. The gate is what auto-merge is trusting. Choose an action's pinning style knowing it decides participation: a floating major tag updates only when a new major tag exists upstream, so an action that stops publishing them stops producing proposals. Pinning to an exact version or a sha puts it back in the automated stream.

## Rationale

Auto-merge is only defensible where the gate is. Every pull request here runs the live suite against a real Bitbucket (ADR-043) and must clear the coverage gates (ADR-065), so a green run exercises the dependency rather than merely compiling against it. Reading the diff of a patch bump adds little to that, and doing it for every proposal adds enough friction that updates accumulate, which is the outcome the policy exists to avoid. The held set is where a green run is not the whole question. A major bump can be green and still change behaviour a reader should see, and the product image tag is a claim about what this project supports rather than a dependency of its build.

## Rejected Alternatives

- `Review every dependency update by hand`: Updates accumulate and land in batches, which is when they break. The gate already exercises them against a real server.
- `Auto-merge majors too`: A green run does not answer whether a behaviour change should be adopted.
