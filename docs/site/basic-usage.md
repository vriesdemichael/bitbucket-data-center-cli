# Basic Usage

## What you can manage

`bb` supports operational workflows across:

- Authentication and server context (`auth`)
- Repository settings and collaboration (`repo`, `reviewer`, `hook`, `branch`, `tag`, `commit`, `ref`)
- Pull requests and quality controls (`pr`, `build`, `insights`)
- Project-level administration (`project`, `admin`)
- Cross-project discovery (`search`)
- Multi-repository policy automation (`bulk`)
- `gh`-style repository ergonomics for Bitbucket (`repo clone`, `browse`)

Use [All Commands](reference/commands/index.md) for complete command and argument coverage.

## Command discovery pattern

```bash
bb --help
bb repo --help
bb repo settings --help
bb repo settings security --help
```

The command reference page is generated from Cobra help output, so usage/flags match CLI behavior.

## Shorter spellings and aliases

Some operations are reachable under a second, shorter name because that is the name people
(and coding agents) reach for first. Both spellings run the same command and produce
identical output; each one's `--help` names the other so you can discover either form.

```bash
bb pr diff 42
bb repo create --project TEST --name my-repo
bb repo fork --name my-fork --repo TEST/my-repo
bb repo delete --repo TEST/my-fork
bb repo permissions list --repo TEST/my-repo
bb repo permissions grant alice REPO_WRITE --repo TEST/my-repo
bb repo permissions grant --group developers REPO_READ --repo TEST/my-repo
bb project permissions grant TEST alice PROJECT_WRITE
```

| Shorter / Elevated | Deep Path / Alias | Notes |
| --- | --- | --- |
| `bb pr diff` | `bb diff pr` | `bb diff pr` is canonical in reference |
| `bb repo create` | `bb repo admin create` | `bb repo create` is canonical |
| `bb repo fork` | `bb repo admin fork` | `bb repo fork` is canonical |
| `bb repo delete` | `bb repo admin delete` | `bb repo delete` is canonical |
| `bb repo permissions list` / `grant` / `revoke` | `bb repo settings security permissions users …` | `--group` replaces `groups` segment |
| `bb project permissions list` / `grant` / `revoke` | `bb project permissions users …` | `--group` replaces `groups` segment |

`--group` is what replaces the `users` / `groups` path segment on permission commands. Omitting it means a user,
so it is worth being deliberate about on a grant.

## Pull request target resolution

Commands operating on a pull request (`bb pr get`, `bb pr checkout`, `bb pr diff`, `bb pr review`, `bb pr comment`, `bb pr merge`, etc.) resolve the target flexibly:

- **Numeric ID**: `42`
- **Hash prefix**: `#42`
- **Source branch name**: `feature/login`, `refs/heads/feature/login`
- **Full Bitbucket URL**: `https://bitbucket.acme.corp/projects/PRJ/repos/demo/pull-requests/42` (also supports personal repos `~username` and `/diff`, `/commits`, `/overview` subpaths)

When you pass a full PR URL, `bb` automatically extracts the project, repository slug, and pull request ID, so you do not even need to supply `--repo` or stand inside a local clone:

```bash
# Target via full browser PR URL (no local git clone needed)
bb pr get https://bitbucket.acme.corp/projects/PRJ/repos/demo/pull-requests/42

# Diff via PR URL
bb pr diff https://bitbucket.acme.corp/projects/PRJ/repos/demo/pull-requests/42

# Check out via source branch name or hash
bb pr checkout feature/payment-gateway
bb pr checkout #42
```

## `bb pr status`

Not a shorter spelling but a view of its own: the pull requests on your current branch, the
ones you opened, and the ones waiting on your review, across every repository.

```bash
bb pr status
```

The review section shows only what you have not responded to yet — the same set Bitbucket's
own dashboard shows. Pull requests you already approved or sent back as needing work are
waiting on their author, not on you.

The current-branch section needs a git checkout with a Bitbucket remote. Outside one, it
reports why in a `note` and the other two sections still answer.

## `bb pr checkout`

Checks out a pull request's source branch in the repository you are standing in, so you can
run it, review it, and push fixes back.

```bash
bb pr checkout 42
bb pr checkout #42
bb pr checkout feature/login
bb pr checkout https://bitbucket.acme.corp/projects/PRJ/repos/demo/pull-requests/42
bb pr checkout 42 --branch review-42
bb pr checkout 42 --detach
```

Pull requests from a **fork** work too, and are the reason to use this rather than a manual
`git fetch`. `bb` fetches from the fork, adding a remote for it if you do not have one, and
sets the branch upstream so a later plain `git push` goes back to the fork branch the pull
request is built from. The local branch is prefixed with the fork owner (`jdoe/fix-login`)
so it cannot collide with a branch of the same name of your own.

Running it again on the same pull request fast-forwards the branch you already have. If the
branch has diverged from the pull request, that fails rather than merging or discarding
anything.

A working tree with uncommitted changes to tracked files is refused; pass `--force` to
discard them. Untracked files are ignored, so build output does not get in the way.

The fetch uses the credentials `bb` is already authenticated with, so this works immediately
after `bb repo clone` with no git credential setup. The credential is passed to that one git
invocation and never written into the repository.

Pushing afterwards is plain `git`, which does not go through `bb` — run `bb auth setup-git`
once to let it authenticate. See [Git Authentication](advanced/git-authentication.md).

## Reviewers

`bb pr create` pre-fills reviewers the same way the web interface does: default
reviewer conditions for the branch pair, plus code owners matching the diff from
`.bitbucket/CODEOWNERS`. Both are on by default and are skipped without comment
when the repository does not configure them.

```bash
# Default reviewers and code owners are applied automatically
bb pr create --from-ref feature/login --to-ref main --title "Add login"

# Opt out of either or both
bb pr create --from-ref feature/login --to-ref main --title "Add login" --no-default-reviewers --no-codeowners

# Name reviewers and reviewer groups explicitly (repeatable or comma-separated)
bb pr create --from-ref feature/login --to-ref main --title "Add login" --reviewers alice,bob --reviewer-group backend-team

# @group works anywhere a reviewer is accepted
bb pr create --from-ref feature/login --to-ref main --title "Add login" --reviewers alice,@backend-team
```

The same automation is available for a pull request that already exists, where it
is opt-in rather than default:

```bash
bb pr review reviewer add 42 --user alice --user bob --reviewer-group core-team
bb pr review reviewer add 42 --default-reviewers --codeowners
```

Reviewer groups need Bitbucket Data Center 7.13+, `.bitbucket/CODEOWNERS` needs
8.14+, and `bb` reads both with the syntax and precedence Bitbucket documents —
including the `:all`, `:random(N)` and `:least_busy(N)` group selectors. Where it
necessarily differs from the browser:

- Reviewers are resolved on the client, because `POST /pull-requests` does not
  evaluate conditions or CODEOWNERS server-side. Everything is decided before the
  pull request is created.
- A `CODEOWNERS` file in your working copy is used in preference to the server's
  copy, but only when that working copy is a checkout of the repository you are
  targeting.
- `least_busy(N)` ranks candidates by their open unapproved reviews **in the
  target repository only**. The browser has no equivalent to fall back on, so when
  that ranking cannot be computed `bb` warns and assigns in group order instead.
- Because both behaviours are on by default, a lookup that fails for a reason
  other than "not configured" warns on stderr and the pull request is still
  created. Pass `--default-reviewers` or `--codeowners` explicitly to make such a
  failure fatal.

## Repository context behavior

- `--repo PROJECT/slug` has highest precedence. `--repo` also accepts full Bitbucket repository URLs (`https://bitbucket.acme.corp/projects/PRJ/repos/demo`) and personal user repositories (`~username/slug`).
- If `--repo` is omitted, `bb` can infer repository context from local git remotes that match authenticated hosts.
- When several remotes match, `origin` wins — a fork or mirror alongside it does not make the context ambiguous.
- An `upstream` remote is the exception: it conventionally outranks `origin`, so having both is a genuine ambiguity and `bb` asks for explicit selection.

See [Advanced: Repository Discovery and Server Switching](advanced/repository-discovery-and-server-switching.md)
for remote URL formats, precedence, ambiguity handling, and multi-server workflows.

## Strict non-interactive contract

`bb` operates strictly non-interactively across all commands ([ADR-054](adr/054-strict-non-interactive-cli-contract.md)).
Commands never block on standard input for interactive prompts or confirmation dialogs (`[y/N]`). Missing or invalid options fail fast with descriptive error messages, ensuring predictable execution in scripts, CI/CD pipelines, and AI agent tool calls.

## Dry-run behavior and scope

- `--dry-run` applies to server-mutating Bitbucket commands.
- `--dry-run` does not apply to local auth/config mutators.
- Dry-run output includes explicit planning metadata such as planning mode and capability signaling.
- For bulk workflows, `bulk plan` is the preview mechanism and `bulk apply` executes reviewed plans.

See [Advanced: Dry-Run Planning](advanced/dry-run-planning.md) for safety and contract details.

## Machine mode (`--json`)

- Machine responses are wrapped in a versioned envelope:

```json
{
  "version": "v2",
  "data": {},
  "meta": {
    "contract": "bb.machine"
  }
}
```

- `data` contains the command-specific payload shape.
- Contract changes are additive within version `v2`; breaking changes require a version bump.

Example machine output (`bb --json auth status`):

```json
{
  "version": "v2",
  "data": {
    "bitbucket_url": "https://bitbucket.acme.corp",
    "bitbucket_version_target": "9.4.16",
    "auth_mode": "token",
    "auth_source": "stored/default"
  },
  "meta": {
    "contract": "bb.machine"
  }
}
```

## Config and auth precedence

Runtime precedence order:

1. CLI flags
2. Environment variables / `.env`
3. Git remote inference (repo + host context)
4. Stored config (`~/.config/bb/config.yaml`) + keyring/fallback secrets
5. Built-in defaults

Supported day-to-day authentication modes are token and basic auth.

That precedence governs how `bb` authenticates to the Bitbucket API. Plain `git`
authenticates separately: it does not read `bb`'s configuration, so `git push`
and `git pull` inside a clone need `bb auth setup-git` once per host. Credentials
are never written into a repository — see
[Git Authentication](advanced/git-authentication.md).

## Quick examples

```bash
bb --json auth status
bb repo clone TEST/my-repo
bb repo create --project TEST --name my-service
bb repo fork --repo TEST/my-service --name my-service-fork
bb pr get https://bitbucket.acme.corp/projects/TEST/repos/my-service/pull-requests/42
bb pr checkout #42
bb pr diff feature/payments
bb browse --repo TEST/my-repo src/main.go
bb search repos demo --limit 20
bb tag list --repo TEST/my-repo --limit 50
bb --dry-run project create DEMO --name "Demo Project"
```

Example human output (`bb auth status`):

```text
Target Bitbucket: https://bitbucket.acme.corp (expected version 9.4.16, auth=token, source=stored/default)
```
