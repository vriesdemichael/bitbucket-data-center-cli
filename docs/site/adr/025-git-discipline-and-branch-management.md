---
search:
  boost: 0.3
---

# ADR-025: Git discipline and branch management

`next` and `main` keep a linear history. A change reaches `next` through a pull request merged by rebase: the repository allows no merge commit and no squash, and the ruleset on both branches requires linear history and refuses a force push. `main` moves only by promotion (ADR-066). A pull request of more than 100 commits, which GitHub will not rebase-merge, lands by a fast-forward push of its branch.

Branch from `next`. Bring a branch up to date by rebasing it onto `next`, never by merging `next` into it, and give every commit a Conventional Commit subject (ADR-006). A pull request branch is yours to rewrite, by amending, rebasing and force-pushing with lease, as often as it helps; `next` and `main` are never rewritten.

A linear history reads, bisects and reverts one commit at a time. The release takes its version and its notes from the commits themselves, so each one has to say what it is.
