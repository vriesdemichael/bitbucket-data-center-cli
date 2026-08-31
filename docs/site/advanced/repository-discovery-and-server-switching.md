# Repository Discovery and Server Switching

## Why this matters

When you run `bb` inside a local git repository, the CLI can infer `PROJECT/slug` and host
from matching remotes. This reduces repeated `--repo` flags while still keeping behavior explicit.

## Repository discovery behavior

Discovery runs only when a command has a `--repo` flag and you did not set it.

`bb` inspects git remotes and tries to parse Bitbucket-style URLs such as:

- `https://bitbucket.acme.corp/scm/PLAT/payments-api.git`
- `ssh://git@bitbucket.acme.corp:7999/scm/PLAT/payments-api.git`
- `git@bitbucket.acme.corp:scm/PLAT/payments-api.git`

If a remote endpoint matches an authenticated/stored server context or one of its aliases, `bb` infers:

- `BITBUCKET_URL`
- `BITBUCKET_PROJECT_KEY`
- `BITBUCKET_REPO_SLUG`
- and sets the effective `--repo` value to `PROJECT/slug`

Human mode emits a banner on `stderr`:

```text
Using repository context from git remote "origin": PLAT/payments-api on https://bitbucket.acme.corp
```

JSON mode suppresses that banner to preserve machine output contracts on `stdout`.

## Precedence and safety

Repository selection precedence for repo-scoped commands:

1. Explicit `--repo` (accepts `PROJECT/slug`, personal repos `~username/slug`, or full Bitbucket URLs like `https://bitbucket.acme.corp/projects/PROJECT/repos/slug` and `ssh://...`)
2. Git remote discovery (if exactly one matching remote context exists)
3. `BITBUCKET_PROJECT_KEY` + `BITBUCKET_REPO_SLUG`

Host and auth source precedence remains:

1. CLI flags
2. Environment variables / `.env`
3. Git remote inference host override (when `--repo` is inferred from a matching authenticated remote)
4. Stored config (`~/.config/bb/config.yaml`) + keyring-backed credentials
5. Built-in defaults

## Ambiguity and fallback behavior

- If several remotes map to different repositories and one of them is `origin`, `origin` wins. A fork or a mirror alongside `origin` is ordinary — `bb pr checkout` adds one itself — and git's own convention is that `origin` is the repository the clone belongs to.
- An `upstream` remote is the exception. It conventionally outranks `origin`, so a repository with both is genuinely ambiguous: discovery fails with a validation error and asks you to pass `--repo` and/or choose a server.
- If several remotes map to different repositories and none is `origin`, discovery fails the same way.
- If you are outside a git repository, discovery is skipped.
- If remotes do not match authenticated server hosts, discovery is skipped.

## Host aliases

Many Bitbucket instances use different endpoints for browser/API access and git clone traffic.
For example:

- canonical Bitbucket URL: `https://bitbucket.acme.corp`
- SSH clone host: `git.acme.corp:7999`

`bb` stores one canonical server context and can attach one or more aliases to it. Alias matching is
endpoint-aware and normalizes values as `host:port`.

Examples:

- `https://bitbucket.acme.corp` -> `bitbucket.acme.corp:443`
- `http://bitbucket.acme.corp` -> `bitbucket.acme.corp:80`
- `ssh://git@git.acme.corp:7999/scm/PLAT/payments-api.git` -> `git.acme.corp:7999`
- `git@git.acme.corp:scm/PLAT/payments-api.git` -> `git.acme.corp:22`

Manual alias management:

```bash
bb auth alias list --host https://bitbucket.acme.corp
bb auth alias add --host https://bitbucket.acme.corp git.acme.corp:7999
bb auth alias remove --host https://bitbucket.acme.corp git.acme.corp:7999
```

Automatic alias discovery:

```bash
bb auth login https://bitbucket.acme.corp --token "$BB_TOKEN"
bb auth alias discover --host https://bitbucket.acme.corp
```

Discovery is best-effort. It requests only a small repository page and stops at the first accessible
repository that exposes clone links. Login still succeeds when discovery finds no aliases.

Discovery **adds** to the aliases already stored; it never removes one. That matters because
discovery cannot find every alias — an instance whose SSH clone host differs from its web URL is
exactly the case for adding one by hand, and it would otherwise be undone by the next discovery run,
or by the next `bb auth login`. Re-authenticating keeps stored aliases for the same reason.

To store only what discovery finds, ask for it explicitly. Anything dropped is named in the output:

```bash
bb auth alias discover --host https://bitbucket.acme.corp --replace
```

## Server switching workflow

Use server contexts to control which host is active by default:

```bash
bb auth server list
bb auth server use --host https://bitbucket.acme.corp
bb auth status
```

Expected human output:

```text
Active server set to https://bitbucket.acme.corp
Target Bitbucket: https://bitbucket.acme.corp (expected version 9.4.16, auth=token, source=stored/default)
```

Expected JSON output (example):

```json
{
  "version": "v2",
  "data": {
    "status": "ok",
    "default_host": "https://bitbucket.acme.corp"
  },
  "meta": {
    "contract": "bb.machine"
  }
}
```

## Recommended team pattern

- Keep one stored context per server (`bb auth login <host>`).
- Let `bb auth login` auto-discover clone-host aliases when possible.
- Add explicit aliases for non-default SSH endpoints when discovery does not surface them.
- Switch active context with `bb auth server use --host ...` before running automation.
- Still pass `--repo` in CI for maximal explicitness.
