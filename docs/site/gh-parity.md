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
| `gh pr edit 42` | `bb pr update 42` | `bb pr edit` works as an alias. Pass `--version` to have the update refused if the pull request changed since you read it |
| `gh pr close 42` | `bb pr decline 42` | `bb pr close` works as an alias |
| `gh pr checks 42` | `bb pr build status 42` | `bb pr checks` works as an alias |
| `gh pr merge 42` | `bb pr merge 42` | |
| `gh pr diff 42` | `bb pr diff 42` | |
| `gh pr review` | `bb pr review approve`, `bb pr review unapprove`, `bb pr review set` | `set <pr-id> <status>` is the general form; approve and unapprove are the two shorthands |
| `gh pr comment` | `bb pr comment add` | |
| `gh pr checkout 42` | `bb pr checkout 42` | |
| `gh pr ready 42` | `bb pr ready 42` | `--undo` turns it back into a draft |
| `gh pr status` | `bb pr status` | The current branch's pull request, the ones you opened, and the ones waiting on your review |
| `gh pr reopen 42` | `bb pr reopen 42` | Reopens a declined pull request |
| `gh pr merge 42 --auto` | `bb pr auto-merge enable 42` | A subcommand rather than a flag: `disable` and `get` are the other two. Needs Bitbucket Data Center 8.0 or newer |

## Repositories

| `gh` | `bb` | Notes |
|---|---|---|
| `gh repo clone` | `bb repo clone` | |
| `gh repo list` | `bb repo list` | |
| `gh repo view` | `bb repo get` | `bb repo view` works as an alias. The README is printed as raw markdown rather than rendered; `--readme=false` leaves it out. There is no `--web`: `bb browse` opens the repository |
| `gh repo create` | `bb repo create` | |
| `gh repo fork` | `bb repo fork` | `--project` chooses where the fork lands; without it the fork goes to your personal project |
| `gh repo delete` | `bb repo delete` | `--yes` skips the confirmation, and only when the repository is named rather than inferred |
| `gh browse` | `bb browse` | Opens repository pages in a browser. Not to be confused with `bb repo browse`, which reads file content over REST rather than opening anything |

## Anything else

| `gh` | `bb` | Notes |
|---|---|---|
| `gh api` | `bb api` | The same escape hatch under the same name. It speaks Bitbucket's REST API, so the paths are Bitbucket's — `/rest/api/latest/...` rather than GitHub's |
| `gh completion` | `bb completion` | |
| `gh version` | `bb --version` | |
| `gh search repos` | `bb search repos` | `bb search commits` and `bb search prs` are the other two. There is no code search |
| `gh ssh-key` | `bb ssh-key` | `add`, `list` and `remove`, for your own keys. `bb repo ssh-key` manages a project's or repository's access keys, which is a different thing |
| `gh gpg-key` | `bb auth gpg-key` | Under `auth`, not at the top level. `add`, `list`, `remove` and `clear` |
| `gh release` | — | Bitbucket Data Center has no releases. `bb tag` is the nearest thing, and `bb browse --releases` opens the tags page |
| `gh run` | — | Bitbucket Data Center runs no CI of its own. External CI reports in through build statuses: `bb build` and `bb pr build status` read them |

## Authentication

| `gh` | `bb` | Notes |
|---|---|---|
| `gh auth login` | `bb auth login <host>` | The host is an argument: `bb` is built for self-hosted instances, so there is no default one |
| `gh auth status` | `bb auth status` | |
| `gh auth logout` | `bb auth logout` | |
| `gh auth setup-git` | `bb auth setup-git` | |
| `gh auth switch` | `bb auth server use` | Sets which stored host is the default. `bb auth server list` shows them |
| `gh auth token` | — | `bb auth token` is not this. It manages Bitbucket HTTP access tokens on the server — `create`, `get`, `list`, `revoke`, `update` — and never prints the credential `bb` is holding. To see what `bb` would hand git, ask the credential helper, which prints your token in full: `bb auth git-credential get` |

## Issues are not here, and will not be

`gh issue` has no counterpart. Bitbucket Data Center has no issue tracker of its
own — issues live in Jira, a separate product with its own API, its own
permissions and its own CLI surface. Wrapping it would make `bb` a Jira client
that happens to also talk to Bitbucket.

What `bb` does cover is the seam between the two: `bb pr jira` reports the Jira
issues linked to a pull request, because that link is Bitbucket's own data.

## Differences worth knowing

**`-R` is not the repository flag.** In `gh`, `-R` works everywhere. In `bb` the
flag is `--repo`, and `bb browse` is the only command that also accepts `-R`.
Anywhere else `-R` is an unknown shorthand and the command exits `2`.

**A repository is named by project and slug.** `gh` addresses a repository as
owner and name; `bb` takes `--repo PROJECT/slug`, because that is how a
Bitbucket estate is addressed. Like `gh`, `bb` infers one from the git remotes of
the directory you are in, so the flag is usually unnecessary inside a clone.

**Not every command works on a repository.** `bb auth`, `bb search`, `bb doctor`,
`bb ssh-key` and `bb project` address a host, an estate or a project, and take no
`--repo` at all. `bb pr status` is a mixed case: two of its three sections are
cross-repository, and the third reports itself unavailable rather than failing
when you are not in a checkout.

**`--json` is a flag, not a field selector.** In `gh`, `--json` takes a list of
fields. In `bb` it takes nothing and emits the whole envelope; see
[Machine Mode and Diagnostics](advanced/machine-mode-diagnostics.md).
