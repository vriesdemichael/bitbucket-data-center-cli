---
search:
  boost: 1.8
---

# Coming from `gh`

`bb` is built to feel like `gh`, and mostly does. This page covers the places
where the two differ, and the one area `bb` deliberately does not cover.

Where the spelling differs, the `gh` name usually works as an alias. The `bb`
name is the one the documentation uses, because it names its subject in the
path rather than in a flag.

## Pull requests

| `gh` | `bb` | Notes |
|---|---|---|
| `gh pr create` | `bb pr create` | |
| `gh pr list` | `bb pr list` | |
| `gh pr view 42` | `bb pr get 42` | `bb pr view` works as an alias |
| `gh pr edit 42` | `bb pr update 42` | `bb pr edit` works as an alias. `--version` is required — Bitbucket uses optimistic locking, so an update names the version it expects |
| `gh pr close 42` | `bb pr decline 42` | `bb pr close` works as an alias |
| `gh pr checks 42` | `bb pr build status 42` | `bb pr checks` works as an alias |
| `gh pr merge 42` | `bb pr merge 42` | |
| `gh pr diff 42` | `bb pr diff 42` | |
| `gh pr review` | `bb pr review approve`, `bb pr review unapprove`, `bb pr review set` | `set <id> <status>` is the general form; approve and unapprove are the two shorthands |
| `gh pr comment` | `bb pr comment add` | |
| `gh pr checkout 42` | `bb pr checkout 42` | |
| `gh pr ready 42` | `bb pr update 42 --draft=false` | Bitbucket treats draft as a field rather than a state transition, so this is an update rather than its own verb |

## Repositories

| `gh` | `bb` | Notes |
|---|---|---|
| `gh repo clone` | `bb repo clone` | |
| `gh repo list` | `bb repo list` | |
| `gh repo view` | no single equivalent | `gh repo view` shows a repository's description and renders its README. `bb browse` opens the repository in a browser, `bb repo cat README.md` prints the README, and `bb repo list` shows a project's repositories with their descriptions |
| `gh repo create` | `bb repo create` | |
| `gh browse` | `bb browse` | Opens repository pages in a browser. Not to be confused with `bb repo browse`, which reads file content over REST rather than opening anything |

## Authentication

| `gh` | `bb` | Notes |
|---|---|---|
| `gh auth login` | `bb auth login <host>` | The host is an argument: `bb` is built for self-hosted instances, so there is no default one |
| `gh auth status` | `bb auth status` | |
| `gh auth logout` | `bb auth logout` | |
| `gh auth setup-git` | `bb auth setup-git` | |

## Issues are not here, and will not be

`gh issue` has no counterpart. Bitbucket Data Center has no issue tracker of its
own — issues live in Jira, a separate product with its own API, its own
permissions and its own CLI surface. Wrapping it would make `bb` a Jira client
that happens to also talk to Bitbucket.

What `bb` does cover is the seam between the two: `bb pr jira` reports the Jira
issues linked to a pull request, because that link is Bitbucket's own data.

## Differences worth knowing

**Every command takes a repository.** `gh` infers one repository from the
directory. `bb` does too, and it also accepts `--repo PROJECT/slug`, because a
Bitbucket estate is addressed by project and repository rather than by owner and
name.

**There is no `gh api` equivalent by that name.** `bb api` is the escape hatch,
and it speaks Bitbucket's REST API.

**`--json` is a flag, not a field selector.** In `gh`, `--json` takes a list of
fields. In `bb` it takes nothing and emits the whole envelope; see
[Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md).
