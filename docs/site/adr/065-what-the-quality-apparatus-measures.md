# ADR 065: What the quality apparatus measures, and why each part exists

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `065`
- Title: `What the quality apparatus measures, and why each part exists`
- Category: `development`
- Status: `accepted`
- Supersedes: `005`
- Provenance: `guided-ai`
- Source: `docs/decisions/065-what-the-quality-apparatus-measures.yaml`

## Decision

Record the intended shape of the quality apparatus as a whole. It grew one mechanism at a time, each added after a specific escape, and had never been reasoned about as a system. This states what each axis catches that no other does, when an artifact is committed, where thresholds live, and what is deliberately not measured.
1. Coverage axes, and the distinct question each answers:

   - Patch line coverage blocks new untested code, with a lower bar for patches too small for a
     percentage to mean anything. It is the axis that fires most often and it earns its cost: it
     caught unreachable defensive branches that would otherwise have shipped as code no test could
     exercise. The thresholds live in .github/coverage-thresholds.env.
   - Global line coverage, scoped to cmd/ and internal/ excluding generated paths, answers a
     different question from the patch gate and is not a stricter version of it. Its job is
     erosion: a deleted test, or a refactor that drops whole paths, neither of which the patch gate
     sees. That the two floors happen to be equal is coincidence, not design.
   - Raw line coverage, the same measurement with no scope applied, is still computed and printed
     but is labelled as including generated code, and is not gated. It reads far lower than the
     scoped number because most of the statements in the tree are the generated OpenAPI client and
     models. Unlabelled it read as the project failing badly.
   - Spec coverage records which Bitbucket endpoints the CLI calls at all.
   - Command reach records which commands a live test invokes against a real server without
     skipping on error. It is named for what it measures. It was called CLI live coverage, and at
     100% that name claimed something impossible: the measurement is binary per command and says
     nothing about flags, branches, or paths within a command. What it does catch is a command no
     live test exercises at all, which is a real and previously realised failure.
   - Output-schema coverage, meaning which commands publish a data contract, is the one axis with
     no mechanism.
   - Generated used-operation contract coverage is removed, along with its manifest. It resolved
     each called operation against a hand-written map of operation to test files and counted the
     operation as covered when the list was non-empty -- verifying neither that the file existed
     nor that it touched the operation. It ran against a threshold of zero and covered a minority
     of the operations it measured: the shape of a checklist someone abandoned. A metric that
     cannot fail is not a gate.

2. Committed or recomputed: ADR-045's measurement and baseline distinction is the project-wide
   rule, with one addition. A measurement is the output of running the suite, so recompute it and
   never commit it. A baseline is an assertion about what must remain true, so commit it, because
   a diff in it is what a reviewer needs to see.

   The addition: a committed baseline must have a CI step that fails when the committed copy is
   stale. A baseline with no verification is not a baseline, it is a file that goes quietly wrong.
   generated-operation-contracts.json was the one artifact breaking this rule, and deleting it is
   what brings the set into line rather than adding a ninth mechanism to police it.

3. Thresholds have one home, .github/coverage-thresholds.env. They were in three, that file plus
   Taskfile.yml vars plus ADR-005's prose, so a local run and CI could disagree about what the
   gate was. The Taskfile reads the file; this record states what the numbers mean and does not
   restate their values.

4. The local gate list and the CI gate list are the same list, and that is enforced by a test
   rather than by attention. They had drifted in both directions: three gates ran only locally,
   which made them advisory, and two ran only in CI. A gate that runs only in a git hook is not a
   gate, because nothing stops a branch that skipped the hook.

5. Linear history is enforced by the branch ruleset on main rather than by a CI job. ADR-025
   requires rebase-only integration and still stands; what changed is where that is enforced. The
   ruleset now requires linear history and permits only rebase merges, which decides the question
   at merge time rather than observing a branch after the fact, and cannot be reached around.

   The CI job that previously checked it is removed. It was recorded in ADR-030, already
   superseded, and existed to stop the committed coverage artifacts conflicting on every rebase --
   artifacts ADR-045 deleted. Nothing else depends on it: not the live suite, not the coverage
   gates, not the parity test.

   This is the general preference and not a one-off. Where the platform can enforce a rule at the
   point of the action, prefer that to a CI job that can only observe the result.

6. CI reports the live suite, the coverage gates and the Codecov publication as three jobs rather
   than one. They fail for unrelated reasons, and as one job a patch-coverage breach was reported
   as "Live Integration Tests failed", which sends the reader looking for a broken test that does
   not exist. The live job hands the coverage profiles on as a build artifact.

7. Deliberately not measured, so the question stops recurring: mutation testing, live-suite flake
   rate, dependency freshness, binary size, and startup time. Also not measured, and the more
   interesting omission, is whether a test asserts anything worth asserting. No gate can judge
   that. It is addressed by exercise instead, by deliberately breaking a governance test to
   confirm it fails (issue 484), and that is a habit rather than a mechanism.

## Agent Instructions

Do not add a ninth mechanism without first saying which axis it belongs to and what it catches that the existing ones do not. More coverage is not an answer. When adding a gate that needs no Bitbucket instance, add it to quality:verify in Taskfile.yml. Do not add it to the CI workflow directly; the workflow runs the same list and a test enforces that the two match. A gate that needs a Bitbucket instance goes in the live-tests job and not in a git hook, per ADR-045. When adding a committed artifact under docs/quality/, add its verify task in the same change. If it cannot be verified without a Bitbucket instance, it does not belong there. Do not add a coverage threshold to Taskfile.yml or to a workflow. Add it to .github/coverage-thresholds.env, which both read. Do not lower a threshold to make a change pass. When patch coverage fails, read the uncovered lines the gate prints. If they are unreachable, the code is wrong rather than the gate: extract the decision into something a test can reach, as the update killswitch origin naming was. A metric whose threshold is 0 must not be described as a gate in documentation or in a pull request. Say that it is reported, or delete it. Name a metric for what it measures. Command reach counts commands reached, not lines covered within them; do not reintroduce a name that claims more than the measurement supports.

## Rationale

The apparatus is unusually strong and every part of it was added for a reason that still holds. What had not happened was a pass over the result, and three problems were only visible from that altitude.
Gates that ran in only one of the two places were the costly one. openapi:verify, openapi:operation-paths:verify and docs:verify-generated ran only in the pre-push hook; models:verify and client:verify ran only in CI. The second kind merely wastes a round trip. The first kind is worse in a way that took a concrete incident to see.
A local-only gate is not just advisory, it is unfalsifiable. openapi:operation-paths:verify reported generated-operation-paths.json as 468 operations out of date on a clean checkout of main. It was believed, because there was nowhere to cross-check it: CI did not run the gate, so CI could not contradict it. The artifact was regenerated and the 468 additions committed before anyone noticed that the tool behind it walks the entire working tree, and that this working tree held eight gitignored agent worktrees containing 2,569 Go files from other branches. The gate was reading other branches call sites as if they were this one. Nothing was stale; the regeneration was wrong and had to be reverted.
That is the argument for parity stated more sharply than "a hook can be skipped". A gate running in both places is checked by the disagreement between them. A gate running in one place is checked by nobody, and its false positives are indistinguishable from its true ones.
Thresholds in three places had not yet caused an incident, which is the reason to fix it now rather than after one. A local run reading Taskfile vars and a CI run reading the env file can disagree silently, and the failure mode is a developer who believes a gate passed.
Line coverage keeps its floor but not for the reason usually given. The patch gate demonstrably catches new untested code. The global floor runs with only a couple of points of headroom, and ADR-049 measured that widening the scope to tools/ would cut that to about one. It is therefore close enough to bite on an unrelated change, which is tolerable only because its job is narrow and now stated: erosion, not new code. Thin headroom is a signal to add tests, never to lower the floor.
Naming what is not measured is the cheapest part and prevents the most recurring argument. The honest admission is the last one: nothing here can tell a test that asserts something from a test that merely runs. A live test asserting a message that a unit test had already pinned stayed green for exactly as long as the message did not change, and a killswitch test next to it asserted only an error kind and would have passed against any wording at all. That is a habit problem, and a ninth mechanism would not have caught either.

## Rejected Alternatives

- `Have CI run task quality:verify as a single step instead of enumerating the gates`: It would make drift structurally impossible, which is the right instinct, but it collapses eleven named steps into one in the workflow UI and the pull request summary. Losing which gate failed at a glance is a real cost paid on every failure, against a drift that a test catches for free. The test keeps both properties.
- `Drop the global line coverage floor and rely on the patch gate`: The patch gate cannot see a deleted test or a refactor that removes covered paths wholesale, because neither appears as uncovered changed lines. The floor costs nothing extra to compute, being the same run, and its only real cost is a false failure when headroom is thin, which is itself the signal that tests are owed.
- `Raise the contract coverage threshold instead of deleting the axis`: A floor it already passes enshrines the status quo and creates a second metric that cannot fail; a floor it does not pass blocks every pull request on unrelated work. Neither is worth having when the underlying map is hand-written and unverified: the honest version of this axis is "which generated operations that we call are exercised by a test", computed rather than declared, and that is a different mechanism which nobody has asked for.
- `Stop committing spec-coverage.json and command-reach.json`: Already rejected by ADR-045 and still right. They are verifiable with no Bitbucket instance, so they gate cheaply on every pull request, and command-reach.json has already caught a real regression that stayed green for years.
