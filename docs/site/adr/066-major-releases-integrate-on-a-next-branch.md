---
search:
  boost: 0.3
---

# ADR 066: Every change integrates on next, and only main releases

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `066`
- Title: `Every change integrates on next, and only main releases`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/066-major-releases-integrate-on-a-next-branch.yaml`

## Decision

Work accumulates on a permanent branch named `next`, and a release is cut by fast-forwarding `main` onto it.
Every change targets `next`, whatever version it would cut, dependency updates included. Only `dependabot/*` and `hotfix/*` may open a pull request into `main`, and only carrying no breaking change; the release-flow job refuses the rest and says so. Dependabot opens security updates against the default branch whatever its configuration says: retarget one to `next`, or, when it should ship before the next bundle, merge it into `main` as a `fix(deps):` commit -- `chore(deps)` cuts no release -- and rebase `next` afterwards.
Nothing on `next` releases. The release workflow triggers on a push to `main` only, so `next` collects conventional commits, `feat!` and `BREAKING CHANGE:` among them, without cutting a tag. That is the whole mechanism: ADR-033 releases from every conventional commit on `main`, so ten fixes landing there are ten releases and a breaking change would ship the moment it merged. Bundling is not only for majors -- most bundles are minors, and a branch that releases nothing is what makes any of them possible.
A release is cut by rebasing `next` onto `main` and pushing `next:main`. It is a push and never a pull request: `main` allows only rebase-merge, which replays every commit with a new sha while `next` keeps the originals, leaving two copies of one history. Two consequences belong to the decision rather than to the accident of it. The push is made by an administrator bypassing `main`'s protection, because that protection gates pull requests and this is not one, so `task release:promote:check` asks for the `CI Complete` conclusion the push would otherwise skip. And rebasing `next` rewrites a branch other work sits on, which ADR-025 otherwise reserves for pull request branches: open pull requests into `next` are rebased after a promotion.
Whether a change is breaking no longer decides its branch, but it decides the version the bundle cuts, so it has to be marked. A change that alters an exit code, an error kind, a flag's meaning or the shape of parsed output, or that rejects an invocation which used to succeed, is breaking even when it is a bug fix. Two things are not. Restoring behaviour an accepted ADR or a shipped release note already specified is a fix: the promise was made at that release and the binary did not keep it. And closing a security hole may ship in a minor even though it refuses something that used to be accepted, with release notes saying plainly what stopped working -- ADR-084 removes the unsafe form immediately for the same reason, and holding hardening for the next major leaves the hole open for months.
`next` is gated exactly like `main`: the same branch protection, the same required `CI Complete`, and the full suite including the live tests. CI triggers on `pull_request` and `push` for both branches and nothing else, so a pull request into any other branch reports no checks at all.

## Agent Instructions

Target `next` for every change, a Dependabot pull request included: retarget one opened against `main` rather than merging it there, unless it is a security update that should ship now, which goes in as `fix(deps):` with `next` rebased afterwards. Mark a change breaking when it would fail a command line that works today. This is enforced rather than trusted: the release-flow job refuses a pull request into `main` from anything but `dependabot/*` or `hotfix/*`, and refuses any of those carrying a breaking commit. It reads the same classification the release workflow does, so the gate and the version it protects cannot disagree about what breaking means. Do not open a pull request whose base is another feature branch. CI triggers only on `main` and `next`, so a stacked pull request runs nothing and merges having proven nothing. Do not add a release trigger to `next`, and do not tag from it. The absence of one is what makes batching possible. When adding a branch to the CI workflow triggers, add it to both the pull_request and push lists. Adding only one leaves either pull requests or the merged result unverified.

## Rationale

Two facts about this project make the batching necessary rather than stylistic. ADR-033 cuts a release from every conventional commit on `main`, and ADR-030 keeps history linear. Together they mean a change has nowhere to wait: merging it releases it. Spending a major on each breaking change would be worse for adopters than spending one on all of them, and it would make the migration notes a series of fragments rather than a document somebody reads once.
The branch releasing nothing looks like an omission, which is why it is stated: `next` will collect `feat!` commits for weeks and tag nothing, and somebody tidying the workflows should know that is deliberate before they fix it.
Gating `next` identically to `main` is the part most easily skipped, and it was skipped here before this record existed: the workflow triggered on `main` only, so two pull requests carrying a restructured CI pipeline ran no checks at all and were nearly merged unverified. An ungated branch is worse than an obviously unbuilt one, because the pull request looks finished.

## Rejected Alternatives

- `Release each breaking change as its own major`: Honest under semver and unusable in practice. Adopters would cross several majors in a few months, each with its own migration note, and package managers would carry a version history in which the number stops meaning anything.
- `Keep the work on main behind flags or compatibility shims`: Moves the batching problem into the code, where it is permanent. A shim outlives the migration it was written for, and the CLI would carry both behaviours indefinitely.
- `Let next release prereleases so the work can be tried early`: The Sigstore certificate identity is pinned client-side to the release workflow on the main branch reference, and that pin is compiled into every shipped binary. A release cut from another branch would carry a different identity and fail verification for anyone who already has bb installed. The live suite at full command reach is the gate instead.
- `Give next a lighter CI configuration to keep iteration fast`: The branch accumulates every breaking change in the release. A reduced gate there defers the cost to the merge into main, where the whole batch fails at once and the failure is hardest to attribute.
