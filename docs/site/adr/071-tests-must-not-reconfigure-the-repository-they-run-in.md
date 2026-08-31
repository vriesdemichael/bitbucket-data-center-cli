# ADR 071: Tests must not reconfigure the repository they run in

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `071`
- Title: `Tests must not reconfigure the repository they run in`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/071-tests-must-not-reconfigure-the-repository-they-run-in.yaml`

## Decision

A test that shells out to git operates on a directory it created, never on the working copy. Any package whose tests start a git process installs the guard in internal/git/gittest, which snapshots the ambient git configuration before the run and fails the package if it differs after. Guard also places the repository out of git's reach for the run, by setting a ceiling at its root, so a command that would find it by searching upward fails instead. Both incidents took that shape. Prevention narrows the class; the comparison stays, because an explicit path still reaches the repository and a sibling worktree is not this process at all. The guard reports; it does not repair. It compares before and after rather than checking a list of forbidden keys, so a key nobody anticipated is still caught, and it scrubs the git environment variables that would otherwise leak scope into a subprocess. Which packages need it is computed from the tree, not listed. A list only covers the packages someone remembered.

## Agent Instructions

Create a temporary repository for any test that runs git. Never point one at the working copy, and never at a path derived from the working directory. Install it as func TestMain(m *testing.M) { gittest.Guard(m) }. Do not hand-write the snapshot and comparison; a copied block drifts. Add the guard when you add the first git-invoking test to a package. The governance test computes the set and will tell you. If the guard fires, believe it and find the write. Do not make it repair what it found, and do not narrow it to the keys you expected.

## Rationale

This project is unusually exposed to the class. Writing git configuration is part of what bb does -- credential helpers, clone authentication, remote setup -- so its tests exercise precisely the code that mutates config, and a test given the wrong directory writes into the developer's own repository rather than into a fixture. The damage does not announce itself as a test failure. It surfaces later, somewhere else, as authentication that fails against an unrelated remote or as commits attributed to a fixture identity. Both read as a problem with the thing that broke rather than with the test that caused it, which is why the constraint is enforced by machinery instead of documented as care. It has happened twice. The second time the guard already existed and worked, and was simply not installed on the package that needed it -- which is the argument for computing the set.

## Rejected Alternatives

- `Document the rule and rely on review`: It was documented at length, and the second incident happened anyway.
- `Have the guard undo what it detects`: A guard that repairs is a guard nobody investigates, and it cannot know which writes were the test's.
- `Snapshot global and system configuration too`: Out of scope for a test run and slow. Local and worktree scope is where a misdirected test writes.
