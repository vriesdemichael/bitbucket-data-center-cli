# ADR 066: Major releases integrate on a next branch, and only main releases

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `066`
- Title: `Major releases integrate on a next branch, and only main releases`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/066-major-releases-integrate-on-a-next-branch.yaml`

## Decision

A breaking major release is assembled on a long-lived integration branch named `next`, and is published only when `next` reaches `main`.
1. Where work goes:
   - Every change for the next major targets `next`. Every v3.x patch and non-breaking change
     targets `main`.
   - `next` is created from `main` when a major is opened, and rebases onto `main` for the rest of
     its life (ADR-025, rebase-only). It is deleted after the major ships and recreated for the
     one after.
   - Whether a change is breaking is the question that decides its branch. A change that alters an
     exit code, an error kind, a flag's meaning, the shape of parsed output, or that rejects an
     invocation which previously succeeded, goes to `next` even when it is a bug fix. Several of
     the Phase 0 fixes were exactly that: rejecting an out-of-range --expiry-days is a fix, and it
     fails a command line that used to work.

2. Nothing releases from `next`:
   - The release workflow triggers on `push` to `main` only. `next` accumulates conventional
     commits -- including `feat!` and `BREAKING CHANGE:` footers -- without cutting a single tag.
   - This is the property that makes the branch safe to batch on. ADR-033 releases from every
     conventional commit on `main`, so without a branch that does not release, a breaking change
     could not be staged at all: it would ship the moment it merged.
   - The major is cut by merging `next` into `main`. Every accumulated commit lands at once and
     the existing automation reads the breaking markers among them, so the version bump is
     computed rather than chosen. Nothing about the release path is special-cased for a major.

3. `next` is gated exactly like `main`:
   - CI triggers on `pull_request` and `push` for both branches, and nothing else. A pull request
     into any other branch reports no checks at all -- not one job, and no `CI Complete`, which is
     the single required status.
   - The same branch protection `main` carries applies to `next`: required linear history,
     rebase-only merges, and `CI Complete` required.
   - The full suite runs on `next`, including the live tests. A branch that will become a release
     is not a place for a reduced gate.

## Agent Instructions

Target `next` for anything belonging to the next major, and `main` for v3.x patches and non-breaking work. When unsure, ask whether the change would fail a command line that works today; if it would, it belongs on `next`. Do not open a pull request whose base is another feature branch. CI triggers only on `main` and `next`, so a stacked pull request runs nothing and merges having proven nothing. Rebase onto the integration branch and target it directly. Do not add a release trigger to `next`, and do not tag from it. The absence of one is what makes batching possible; adding it would ship each breaking change as it lands, which is the outcome the branch exists to avoid. When adding a branch to the CI workflow triggers, add it to both the pull_request and push lists. Adding only one leaves either pull requests or the merged result unverified. Mark breaking commits properly -- `!` or a `BREAKING CHANGE:` footer -- on `next` as well as on `main`. The release that eventually reads them is computed from the commits, and a breaking change recorded as a plain fix produces the wrong version at the moment it matters most.

## Rationale

Two facts about this project make the batching necessary rather than stylistic. ADR-033 cuts a release from every conventional commit on `main`, and ADR-030 keeps history linear. Together they mean a breaking change has nowhere to wait: merging it releases it. Spending a major on each one would be worse for adopters than spending one on all of them, and it would make the migration notes a series of fragments rather than a document somebody reads once.
The branch releasing nothing is the whole mechanism, and it is worth stating because it looks like an omission. `next` will collect `feat!` commits for weeks and tag nothing; that is correct, and someone tidying the workflows should know it is deliberate before they "fix" it.
Gating `next` identically to `main` is the part most easily skipped, and it was skipped here before this record existed: the workflow triggered on `main` only, so two pull requests carrying a restructured CI pipeline ran no checks at all and were nearly merged unverified. A branch that will become a release, carrying every breaking change at once, is the last place a reduced gate belongs -- and an ungated branch is worse than an obviously unbuilt one, because the pull request looks finished.

## Rejected Alternatives

- `Release each breaking change as its own major`: Honest under semver and unusable in practice. Adopters would cross several majors in a few months, each with its own migration note, and package managers would carry a version history in which the number stops meaning anything. Issue 486 raises that as an existing problem at 127 tags; this would make it worse.
- `Keep the work on main behind flags or compatibility shims`: Moves the batching problem into the code, where it is permanent. A shim outlives the migration it was written for, and the CLI would carry both behaviours indefinitely. Issue 307 considers a compatibility layer separately; it is not a substitute for a place to stage a release.
- `Let next release prereleases so the work can be tried early`: The Sigstore certificate identity is pinned client-side to the release workflow on the main branch reference, and that pin is compiled into every shipped binary. A release cut from another branch would carry a different identity and fail verification for anyone who already has bb installed. The live suite at full command reach is the gate instead.
- `Give next a lighter CI configuration to keep iteration fast`: The branch accumulates every breaking change in the release. A reduced gate there defers the cost to the merge into main, where the whole batch fails at once and the failure is hardest to attribute.
