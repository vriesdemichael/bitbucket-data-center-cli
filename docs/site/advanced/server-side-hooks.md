# Server-Side Hooks

`bb` does not manage server-side hooks. Not plugin hooks, not hook scripts.

This is a deliberate limit, not a gap waiting to be filled. Both mechanisms
configure code that runs inside Bitbucket on every push, and both are the wrong
tool for the jobs people reach for them to do.

## What is not here

<!-- docs-lint: ignore-inline — this table names commands removed on purpose (ADR-051) -->
| Not in `bb` | What it is |
| --- | --- |
| `bb hook list` / `enable` / `disable` / `configure` | Plugin hooks — pre-receive and post-receive handlers contributed by installed apps |
| `bb repo hook-script list` / `set` / `remove` | Hook scripts — shell scripts uploaded to the server and bound to repository triggers |

Both existed in earlier versions of `bb` and were removed.

## Why

A hook script is a shell script that runs on the Bitbucket server, in the
server's process space, on every push to every repository it is bound to. It is
deployed by upload rather than from version control, so what is running is
whatever was last pushed through an API call — there is no branch, no review, no
history, and no straightforward way to answer "what changed and who changed it".
When it breaks, it breaks pushes for everyone, and it does so on the server
rather than anywhere a developer can see.

Plugin hooks are better behaved, being versioned and installed as apps, but
configuring which are enabled and how is an administrative act performed once
per repository or project. It is not something that belongs in a scripted
workflow, and it is not something a coding agent should be reaching for while
working on a change.

The general form of the argument: pushing enforcement into the server makes it
invisible to the people it applies to. The alternatives are visible — CI that
reports on a pull request, a required build status, a merge check — and they
fail in a place where the person who caused the failure can see it and fix it
themselves.

## What to use instead

**Webhooks.** `bb project webhook` and `bb webhook` stay, and are the supported
way to have Bitbucket tell an external service that something happened. The code
that reacts lives in your repository, runs on your infrastructure, and is
reviewable and revertible like anything else.

```bash
bb project webhook create PROJECT ci https://ci.example.com/hook --event pr:opened
```

**Merge checks and required builds.** For "this must be true before merging",
`bb repo settings` and `bb build` express it where the author will see it.

**The web UI.** Configuring plugin hooks is a one-off administrative task and
the UI is where it belongs.

**Raw API access.** If you have a case that genuinely needs these endpoints —
a migration, an audit, something one-off — a raw REST passthrough is planned
([#330](https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/330))
and is the intended escape hatch. That is deliberate: the endpoints stay
reachable, they just do not get first-class commands that imply they are a good
idea.

## If you disagree

This is an opinionated call, and opinionated calls should be arguable. The place
to argue it is an issue. What would change the decision is a workflow that
genuinely needs these endpoints and cannot be expressed as a webhook, a merge
check, or a CI job — not a preference for doing it from a terminal.
