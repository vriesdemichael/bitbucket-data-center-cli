# ADR 067: Governance tests are verified by breaking them

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `067`
- Title: `Governance tests are verified by breaking them`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/067-governance-tests-are-verified-by-breaking-them.yaml`

## Decision

A governance test asserts an invariant about the codebase rather than a behaviour: every command is classified, every MCP tool has a scope rule, no decision record names a tool that was removed. They are the mechanism that stops this project's recurring failure -- documentation and registries drifting away from the code they describe.
1. Break it before you trust it:
   - Before adding a governance test, break the thing it guards and confirm the test fails. Before
     relying on one you did not write, do the same.
   - Where the sabotage can be expressed as a test, write it as one. That turns "verified once"
     into something CI re-verifies.
   - A guard that survives its own sabotage is either wrong or narrower than it looks. Find out
     which before deleting it: two here looked tautological and only one was.

2. Prefer making a contradiction unrepresentable over testing for it:
   - Where two declarations answer the same question, derive one from the other rather than
     checking them against each other. A derived value cannot disagree, and the test comparing
     them is then the tautology this record exists to prevent.
   - Where they answer different questions, keep both and check both directions. Checking one
     direction leaves the other open, and the open one is not always the harmless one.

3. The list below is the set. It is in this record so that the set is knowable rather than found
   by grepping for a naming convention, which is how three of them were missed the first time
   they were inventoried. TestGovernanceTestsNamedInThisRecordExist fails when this list names a
   test that does not exist, so the list cannot rot the way ADR-039 did.

   - TestAllRunnableCommandsDeclareArgsPolicy: a command whose usage carries a positional
     placeholder declares an Args policy.
   - TestAllCommandsExhaustivelyClassifiedForDryRun: every command is in exactly one dry-run
     category.
   - TestCommandVerbsAgreeWithTheirDryRunClassification: the command name and its category do not
     contradict each other.
   - TestClassifyUsageErrorMatchesCobrasRealMessages: the usage-error markers still match what
     Cobra emits.
   - TestEveryMCPToolIsAccountedFor: every MCP tool maps to a command or is recorded as MCP-only.
   - TestEveryMappedCLICommandExists: no mapping names a command that was removed.
   - TestEveryToolHasAScopeRule: no MCP tool escapes workspace scoping.
   - TestADRDoesNotNameToolsThatDoNotExist: a decision record does not name a tool that was
     removed.
   - TestGatedToolsAreTheOnesThatMergeOrGate: the --yolo set is exactly the tools that merge or
     gate, each with a recorded reason.
   - TestReadOnlyToolsAreNotGated: a tool that writes nothing is not withheld.
   - TestEveryToolHasCallArguments: every MCP tool is called by a test.
   - TestEveryToolReturnsAClientCompatibleResult: a tool result is a JSON object with a text
     fallback.
   - TestEveryHookRunnableGateRunsOnBothSides: every gate a git hook can run runs locally and in
     CI.
   - TestNoGateIsDefinedAndNeverRun: a task named like a check is reachable from something that
     runs it.
   - TestEveryADRCrossReferenceResolves: a decision record does not cite a record that was
     never written.
   - TestGovernanceTestsNamedInThisRecordExist: this list names only tests that exist.

## Agent Instructions

Break a governance test before adding it, and before trusting one you did not write. Record what the invariant is, what breaks it, and that you saw it fail. Add the test to the list in this record in the same change. TestGovernanceTestsNamedInThisRecordExist fails without it, and a governance test nobody can find is one nobody maintains. Do not write a test that compares a value to something derived from it. Check first whether the thing being asserted is already true by construction; if it is, the test cannot fail and the code is where the guarantee lives. When two declarations answer the same question, derive one from the other. When they answer different questions, assert both directions. Do not describe a check as CI-safe. Every check can run anywhere since ADR-043 gave CI its own licensed Bitbucket. What separates the git-hook set from the CI-only set is the cost of booting the stack, not the ability to.

## Rationale

Two of the guards here had stopped guarding anything. TestAllMutatingCommandsHaveDryRunProfile asked whether every mutating command was registered in dryRunProfiles while defining "mutating" as "present in dryRunProfiles", so it asserted that the map contains what the map contains. It ran, passed, and held the slot for a check nobody had. Its replacement compares the classification against the command's own name, which is chosen by whoever adds the command and derived from nothing, so the two can disagree. It found a real ambiguity on its first run: "resolve" writes in `pr comment resolve` and reads in `ref resolve`.
The MCP safety flag and the destructive annotation were checked one way only -- a gated tool claiming to be harmless failed, a tool exposed without --yolo while annotating itself destructive did not. The unguarded direction was the dangerous one, because the annotation is advice a client may ignore while the flag is what the server enforces. That pair is now derived rather than checked, which is the better answer wherever it is available.
The exercise also separated two cases that read identically from the code. TestAllRunnableCommandsDeclareArgsPolicy looked tautological for the same reason as the first: enforceNoArgsDefaults fills in a missing policy immediately before the test reads it. It is not tautological -- the enforcer skips commands whose usage carries a positional placeholder, and those the test does catch. Only running both sabotages told them apart, which is the argument for doing it rather than reasoning about it.
A record that cites a record which does not exist sends a reader nowhere. That is the same failure as naming a test or a tool that was removed, and it is checked the same way.
Keeping the list here rather than only in AGENTS.md is deliberate. Three guards were missed when the set was first inventoried by grepping for TestEvery and TestAll, because they follow neither convention. A list is only worth having if something checks it, which is why this one is checked.

## Rejected Alternatives

- `Centralise the governance tests in one package`: Each currently sits beside the thing it guards, which is where someone changing that thing will trip over it. Moving them together would make the set easy to enumerate and easy to forget, and enumerating it is what this record already does.
- `Keep the list in AGENTS.md only`: AGENTS.md is instructions for doing work, and nothing verifies its contents. A list of guards that goes stale is the ADR-039 failure with a different filename. Putting it here makes it a decision, and the guard on it makes it a checked one.
- `Trust code review to catch a tautological guard`: Review is what let both of them in. They read correctly: the assertion names the invariant, the error message explains it, and the only thing wrong is that the two sides of the comparison come from the same place. That is invisible without running it.
