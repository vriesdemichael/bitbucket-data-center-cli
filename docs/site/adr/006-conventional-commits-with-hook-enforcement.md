---
search:
  boost: 0.3
---

# ADR-006: Conventional Commits, and git hooks run by lefthook

Every commit subject is a Conventional Commit, `type(scope): description`, with `!` or a `BREAKING CHANGE:` footer marking a breaking change. The type decides whether a commit reaching `main` cuts a release, and how large (ADR-033), and the release notes are built from the subjects, so a wrong type ships a wrong version. No hook checks a subject; a reviewer does.

lefthook runs the git hooks configured in `lefthook.yml`: the unit tests before a commit, and before a push the docs build and the gates that need no Bitbucket instance. They are recommended rather than required, because CI runs the same gates on every pull request and CI is what refuses a change; a hook only tells you sooner. Do not skip a hook. A new gate that needs no Bitbucket instance goes into `task quality:verify`, which the pre-push hook and CI both run (ADR-065).

## Not chosen

- **Husky to manage the hooks**: Node-centric, for a project with no Node toolchain.
