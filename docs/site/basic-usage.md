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

## Shorter spellings

Some operations are reachable under a second, shorter name because that is the name people
(and coding agents) reach for first. Both spellings run the same command and produce
identical output; the canonical one is longer but names its subject in the path rather
than in a flag.

Both appear in [All Commands](reference/commands/index.md), and each one's `--help` names
the other, so you can find either from either.

```bash
bb pr diff 42
bb repo permissions list --repo TEST/my-repo
bb repo permissions grant alice REPO_WRITE --repo TEST/my-repo
bb repo permissions grant --group developers REPO_READ --repo TEST/my-repo
bb project permissions grant TEST alice PROJECT_WRITE
```

| Shorter | Canonical |
| --- | --- |
| `bb pr diff` | `bb diff pr` |
| `bb repo permissions list` / `grant` / `revoke` | `bb repo settings security permissions users …` |
| the same with `--group` | `bb repo settings security permissions groups …` |
| `bb project permissions list` / `grant` / `revoke` | `bb project permissions users …` |
| the same with `--group` | `bb project permissions groups …` |

`--group` is what replaces the `users` / `groups` path segment. Omitting it means a user,
so it is worth being deliberate about on a grant.

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

## Repository context behavior

- `--repo PROJECT/slug` has highest precedence.
- If `--repo` is omitted, `bb` can infer repository context from local git remotes that match authenticated hosts.
- If multiple remotes match different repositories, `bb` returns an ambiguity error and asks for explicit selection.

See [Advanced: Repository Discovery and Server Switching](advanced/repository-discovery-and-server-switching.md)
for remote URL formats, precedence, ambiguity handling, and multi-server workflows.

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
bb browse --repo TEST/my-repo src/main.go
bb search repos demo --limit 20
bb tag list --repo TEST/my-repo --limit 50
bb --dry-run project create DEMO --name "Demo Project"
```

Example human output (`bb auth status`):

```text
Target Bitbucket: https://bitbucket.acme.corp (expected version 9.4.16, auth=token, source=stored/default)
```
