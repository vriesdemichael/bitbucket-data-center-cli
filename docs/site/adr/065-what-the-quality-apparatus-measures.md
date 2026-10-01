---
search:
  boost: 0.3
---

# ADR-065: What the quality apparatus measures, and why each part exists

Each part of the quality apparatus answers a question no other part does:

- Patch line coverage blocks new untested code, with a lower bar for a patch too small for a percentage to mean anything.
- Global line coverage, over cmd/ and internal/ without generated code, catches erosion: a deleted test, or a refactor that drops whole paths, neither of which shows as an uncovered changed line. It is not a stricter patch gate.
- Raw line coverage, the same measurement with generated code included, is printed and labelled as such, and not gated.
- Spec coverage records which Bitbucket operations the CLI calls at all.
- Command reach records which commands a live test runs against a real server and asserts on (ADR-004). It is binary per command and says nothing about the flags or paths within one; what it catches is a command no live test exercises.
- `TestEveryCommandIsModelled` requires every runnable command to publish the schema of its result, or to say why it has none.

What is committed and what is recomputed is ADR-045. A rule two places must agree on has one home that both read: the coverage thresholds live in .github/coverage-thresholds.env, which Taskfile.yml loads and the workflow sources, and the workflow takes a missing key as an error rather than a default. This record says what the thresholds mean, not what they are.

Every gate that needs no Bitbucket instance runs in both places: in `task quality:verify`, which the pre-push hook runs, and in a CI job other than the live one. `TestEveryHookRunnableGateRunsOnBothSides` fails when a gate runs on one side only, unless exemptFromParity in tools/gateparity gives the reason, and `TestNoGateIsDefinedAndNeverRun` fails for a task named like a check that nothing runs. A gate that needs Bitbucket runs in the live-tests job and in no git hook. CI reports the live suite, the coverage gates and the Codecov upload as three jobs, because they fail for unrelated reasons, and the test jobs hand their coverage profiles to the gates as build artifacts. Where the platform can enforce a rule at the point of the action, that is preferred to a CI job that can only observe the result: linear history on main and next is enforced by the repository's ruleset and merge settings, not by a job. A tool that walks the tree skips dot directories, where agent worktrees keep other branches whose files would read as this one's.

Deliberately not measured: mutation testing, live-suite flake rate, dependency freshness, binary size and startup time. Nor whether a test asserts anything worth asserting, which no gate can judge; ADR-067 has governance tests broken before they are trusted, which is a habit rather than a mechanism.

Before adding a mechanism, say which axis it belongs to and what it catches that the others do not; more coverage is not an answer. Add a gate that needs no Bitbucket instance to quality:verify in Taskfile.yml and as a step in a CI job outside the live one. Put a coverage threshold in .github/coverage-thresholds.env, never in Taskfile.yml or a workflow, and do not lower one to make a change pass. When patch coverage fails, read the uncovered lines the gate prints. If they are unreachable, the code is wrong rather than the gate: extract the decision into something a test can reach. A metric whose threshold is zero is not a gate; say that it is reported, or delete it. Name a metric for what it measures: command reach counts commands reached, not lines covered within them.

A gate that runs in one place only is checked by nobody, and its false positives cannot be told from its true ones; a gate that runs in both is checked by the disagreement between them. One that runs only in a hook is advisory, because nothing stops a branch that skipped the hook. A threshold stated twice can disagree silently, and the failure is a developer who believes a gate passed. The global floor runs with little headroom, which is tolerable only because its job is narrow; thin headroom is a signal to add tests, never to lower the floor.

## Not chosen

- **Have CI run task quality:verify as a single step**: Drift would be impossible, but the workflow and the pull request summary would show one step in place of one per gate, and which gate failed would not be visible at a glance. The parity test keeps both properties.
- **Drop the global floor and rely on the patch gate**: The patch gate cannot see a deleted test or a refactor that removes covered paths. The floor costs nothing extra to compute, and its false failures come when headroom is thin, which is itself the signal that tests are owed.
- **A hand-written map from each operation to the tests that cover it**: It counts an operation covered when its list is non-empty, whether or not the tests exist or touch it, so it cannot fail. An honest version would be computed, not declared.
- **Run the gates that need Bitbucket in the pre-push hook as well**: They take minutes on every push and cannot pass for a contributor who has not started the stack. CI runs them on every pull request.
