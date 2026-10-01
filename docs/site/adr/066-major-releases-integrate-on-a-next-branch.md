---
search:
  boost: 0.3
---

# ADR-066: Every change integrates on next, and only main releases

`next` is a permanent branch, and every change integrates there through a pull request, whatever version it would cut: features, fixes, security fixes and Dependabot's updates alike. `main` moves only when `next` is promoted onto it, and only `main` releases. The release workflow triggers on a push to `main` and on nothing else, so `next` collects conventional commits, breaking ones among them, and tags nothing. Every releasing commit that reaches `main` cuts a version (ADR-033), so a change needs somewhere to wait: on `next`, a run of fixes and features becomes one release, and a set of breaking changes becomes one major with one migration note.

A promotion is a fast-forward push of `next` to `main`, made by an administrator after `task release:promote:check`, never a pull request. The ruleset on `main` requires a pull request, which an administrator's push bypasses, so the release workflow refuses to release a commit that reached `main` without one unless `CI Complete` succeeded on it, and `task release:promote:check` asks the same before the push. `main` takes no commit that `next` lacks, so the push is always a fast-forward and neither branch is rewritten.

`next` is gated exactly as `main` is. One ruleset covers both, requiring a pull request, a passing `CI Complete` and linear history, and CI runs in full on both, live suite included. CI triggers on pull requests into those two branches and pushes to them, and on nothing else.

Whether a change is breaking decides the version a promotion cuts, so mark it. A change is breaking when it would fail a command line that works today: it alters an exit code, an error kind, a flag's meaning or the shape of parsed output, or it rejects an invocation that succeeded, even as a bug fix. Two kinds of change are not breaking. Restoring behaviour that an accepted record or a shipped release note already specified is a fix, because the promise was made at that release. Closing a security hole may ship in a minor, with release notes saying plainly what stopped working (ADR-084).

Open every pull request against `next`, and retarget one opened against `main`, a Dependabot security update included. Do not base a pull request on another feature branch: it runs no checks and merges having proven nothing. Do not add a release trigger to `next` or tag from it; the branch releasing nothing looks like an omission and is the mechanism. When adding a branch to CI's triggers, add it to both the pull request and the push list, or either the pull request or the merged result goes unverified. CI's Release Flow job refuses a pull request into `main` from `next` or from a fork, and any that carries a breaking change, read the way the release workflow reads it.

## Not chosen

- **Promote by pull request**: `main` allows only rebase-merge, which replays every commit with a new SHA while `next` keeps the originals, leaving two copies of one history.
- **Release each breaking change as its own major**: Honest under semver and unusable in practice. Adopters would cross several majors in a few months, each with its own migration note, and the number would stop meaning anything.
- **Keep the work on main behind flags or compatibility shims**: Moves the batching problem into the code, where it is permanent. A shim outlives the migration it was written for, and the CLI would carry both behaviours indefinitely.
- **Let next release prereleases so the work can be tried early**: The Sigstore certificate identity is pinned in every shipped binary to the release workflow on `main`, so a release cut from another branch would fail verification for anyone who has bb installed. The live suite at full command reach is the gate instead.
- **Give next a lighter CI configuration to keep iteration fast**: Defers the cost to the promotion, where the whole batch fails at once and the failure is hardest to attribute.
