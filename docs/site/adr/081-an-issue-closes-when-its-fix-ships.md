---
search:
  boost: 0.3
---

# ADR-081: An issue closes when its fix ships, not when it merges

An issue closes when the release carrying its fix is published, not when the fix merges. A fix merged to `next` leaves its issue open, with the label "staged on next" and one comment naming the pull request and the release the fix will ship in. The closing keyword goes in the body of the commit that fixes the issue, because `main` takes rebase merges and what reaches the default branch is the commit message, not the pull request description. GitHub closes nothing from a branch that is not the default, so the keyword closes the issue when `next` reaches `main`. Whatever the keywords missed is closed by hand once the release is published.

Do not close an issue because its fix merged to `next`. Write one keyword per issue, `Closes #1, closes #2`: GitHub reads `Closes #1, #2` as one reference and closes only the first. Use the one label for every release, never a label per release, and do not mirror a milestone as sub-issues of a tracking issue; both put the same work in two places, and the copy is the one that goes stale. What remains of a release planned as a milestone is `is:open milestone:<release> -label:"staged on next"`.

Adopters install signed binaries through change approval, and to them a closed issue reads as fixed in a version they can install, which while `next` integrates is false for weeks. An open issue also draws the next reporter to the existing thread instead of a duplicate, and gives them something to subscribe to. The label tells the maintainer the work is done, so one issue answers both without misleading either.

## Not chosen

- **Close on merge and let the milestone say when it ships**: The reporter reads the issue, not the milestone, so duplicates arrive as new issues rather than as comments on the thread that is already open.
- **Label each release separately, such as staged-for-v4**: Every release then leaves behind a label that means nothing afterwards, and the comment already names the release.
- **Cut prereleases from next so merged and shipped converge**: ADR-066 rules that out: the Sigstore certificate identity is pinned to the release workflow on `main`, so a build from anywhere else fails verification.
