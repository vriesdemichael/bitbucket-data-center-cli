---
search:
  boost: 0.3
---

# ADR-071: Tests must not reconfigure the repository they run in

A test that shells out to git operates on a directory it created, never on the working copy. Every package whose tests start a git process installs the guard in internal/git/gittest as its TestMain. The guard drops the variables that name a repository outright, such as an inherited GIT_DIR, and sets a ceiling at the repository root, so a git command that would find this repository by searching upward fails instead. It snapshots the local and worktree configuration before the run and fails the package if it differs after, naming each key. It compares everything rather than checking a list of forbidden keys, so a key nobody anticipated is still caught; the one exception is the bookkeeping a sibling worktree writes into the shared local scope (`branch.*`, `remote.*.fetch`, `lfs.*`). The guard reports; it does not repair. Which packages need it is computed, not listed: `TestAmbientGitConfigGuardIsInstalledWhereTestsShellOutToGit` fails a package whose tests start git without it.

Create a temporary repository, normally `t.TempDir()`, for any test that runs git. Never point one at the working copy, and never at a path derived from the working directory. Install the guard as `func TestMain(m *testing.M) { gittest.Guard(m) }`. Only a TestMain with other work to do, such as the live suite's, compares by hand with `gittest.SnapshotAmbientConfig` and `gittest.Diff`; do not copy that into a package that can use Guard, because a copied block drifts. If the guard fires, believe it and find the write. Do not make it repair what it found, and do not widen what it ignores.

Writing git configuration is part of what bb does -- credential helpers, clone authentication, remote setup -- so its tests exercise exactly the code that mutates configuration, and a test given the wrong directory writes into the developer's own repository rather than a fixture. The damage does not show as a test failure. It surfaces later and elsewhere, as authentication failing against an unrelated remote or commits attributed to a fixture identity, and reads as a problem with whatever broke rather than with the test that caused it. The ceiling narrows the class; the comparison stays, because an explicit path still reaches the repository and a sibling worktree is not this process at all.

## Not chosen

- **Document the rule and rely on review**: Review does not notice a guard that was never installed. Computing the set does.
- **Have the guard undo what it detects**: A guard that repairs is a guard nobody investigates, and it cannot know which writes were the test's.
- **Snapshot global and system configuration too**: They belong to the developer and change under unrelated tools mid-run. Local and worktree scope is where a misdirected test writes.
