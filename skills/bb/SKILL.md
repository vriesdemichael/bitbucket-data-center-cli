---
name: bb
description: Query and mutate Bitbucket Data Center resources from an AI agent. Covers pull requests, review workflows, build statuses, branches, tags, commits, repository search, and code insights.
---

# bb — Bitbucket Data Center CLI

`bb` is a CLI for recent versions of Bitbucket for Data Center with a live-behavior-first design. Use it to query and mutate Bitbucket resources during autonomous coding workflows.

> **Authoritative Command Surface**: Built-in CLI help (`bb --help`, `bb <cmd> --help`) and the MCP tool catalogue (`bb ai mcp tools`) reflect the live runtime command tree. Prefer them when a command option or argument signature needs runtime verification.

## Multi-Repository Bulk Governance

This skill focuses on repository-centric developer workflows. For multi-repository
policy management, declarative schema authoring (`bulk-policy.yaml`), and two-stage
plan/apply rollouts (`bb bulk plan`, `bb bulk apply`), see the dedicated `bb-bulk` skill:

```bash
bb ai skill show bulk
```

## Authentication

Before using `bb`, authenticate against your Bitbucket instance:

```bash
printf '%s' "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin
bb auth status
```

**Never pass a token as a flag value.** `--token <value>` puts the credential in the
process argument list, readable by any local user through `ps` or `/proc`, and into
shell history. Use `--token-stdin`, or set `BITBUCKET_TOKEN` in the environment and
skip `bb auth login` entirely — that is usually the better choice in CI and containers,
since it never writes the credential to disk.

Agents cannot complete OAuth flows. Always use a Personal Access Token (PAT).
Create one at: `bb auth token-url`

`bb auth status` reports `Credential storage:` as `keyring`, `environment`, or
`config-file-plaintext`. The last means no OS keyring was available and the token is on
disk in the config file; say so if you are reporting on the environment's security posture.

## Discovering Commands

Every command has built-in help. Prefer `--help` over guessing:

```bash
bb --help
bb pr --help
bb pr get --help
```

## Flexible Target & URL Resolution

Commands that operate on pull requests (`bb pr get`, `bb pr checkout`, `bb pr diff`, `bb pr build status`, `bb pr review`, `bb pr comment`, `bb pr merge`, etc.) accept flexible targets interchangeably:

1. **Full Bitbucket Browser URLs (Zero Configuration)**:
   When a user pastes a full Bitbucket browser PR URL in a prompt or ticket, pass it directly as the target argument:
   ```bash
   bb pr get https://bitbucket.example.com/projects/MYPROJ/repos/payments/pull-requests/42
   bb pr checkout https://bitbucket.example.com/projects/MYPROJ/repos/payments/pull-requests/42
   bb pr diff https://bitbucket.example.com/projects/MYPROJ/repos/payments/pull-requests/42
   bb pr build status https://bitbucket.example.com/projects/MYPROJ/repos/payments/pull-requests/42
   ```
   `bb` automatically extracts the project key, repository slug, and pull request ID from the URL (including `/diff`, `/commits`, subpaths, and personal user repositories `~user`). **No `--repo` flag and no prior git clone are needed.**

2. **Branch Names**:
   Pass a source branch name directly to resolve the active pull request for that branch:
   ```bash
   bb pr checkout feature/my-work
   bb pr diff feature/my-work
   ```

3. **Numeric IDs & Hash Shorthand**:
   Pass `42` or `#42` (infers repository from local git remotes, or explicit `--repo PROJ/repo`):
   ```bash
   bb pr checkout 42
   bb pr checkout #42
   ```

`bb repo clone` also directly accepts full Bitbucket repository browser URLs:
```bash
bb repo clone https://bitbucket.example.com/projects/MYPROJ/repos/payments
```

## Common Workflows

### 1. Start a feature or check out a pull request

```bash
# Find the right repository
bb search repos "payment"

# Check the default branch exists
bb ref resolve --repo MYPROJ/payments main

# Clone the repository
bb repo clone MYPROJ/payments

# Check for an existing branch
bb branch list --repo MYPROJ/payments --filter feature/my-work

# Or check out an existing pull request locally to build, test, or modify
bb pr checkout 42
bb pr checkout 42 --branch review-42
```

`bb pr checkout` automatically resolves source branches across both same-repository branches and personal forks, fetching the commits and configuring tracking.

### 2. Inspect pull request changes and diffs

When reviewing a pull request or verifying behavior, inspect the unified diff and commit history without needing to fetch manually:

```bash
# View unified patch against target branch
bb pr diff 42

# List commits included in the pull request
bb pr commits 42

# List changed files
bb pr files 42
```

### 3. Open a pull request (optionally as a draft)

```bash
# Open a normal PR
bb pr create --repo MYPROJ/payments --from-ref feature/my-work --to-ref main --title "Add payment retries"

# Open a draft PR (Bitbucket DC 8.0+) — signals work-in-progress, not ready for review
bb pr create --repo MYPROJ/payments --from-ref feature/my-work --to-ref main --title "Add payment retries" --draft

# Assign reviewers at creation (repeatable or comma-separated)
bb pr create --repo MYPROJ/payments --from-ref feature/my-work --to-ref main --title "Add payment retries" --reviewers alice,bob
```

When a draft PR is ready for review, flip the draft flag (the `--version` is the
current PR version for optimistic locking, from `bb pr get`):

```bash
# Mark a draft PR as ready for review
bb pr update 42 --repo MYPROJ/payments --version 3 --draft=false

# Convert an open PR back to draft
bb pr update 42 --repo MYPROJ/payments --version 3 --draft
```

### 4. Retrieve build status, mergeable state, and wait for CI

Before merging or approving, verify CI build statuses and merge readiness:

```bash
# Check CI build statuses directly on the pull request (all pipeline stages)
bb pr build status 42

# Inspect approval status, merge state, and outstanding review blockers
bb pr get --repo MYPROJ/payments 42

# Check CI build status for a specific commit SHA
bb build status get <commit-sha>

# Check which CI builds are mandated by repository merge-check rules
bb build required list --repo MYPROJ/payments
```

```
#42	OPEN	feature/my-work -> main	Add payment retries
Reviewers: 2
Open items: 3 unresolved comments, 1 open task
Needs work: carol
```

```bash
# Machine-readable: one field to branch on
bb pr get --repo MYPROJ/payments 42 --json | jq .data.review_summary.action_required

# Check if any PR builds are still INPROGRESS or FAILED
bb pr build status 42 --json | jq '.data[] | {key, state}'
```

`bb pr list` shows the same signal per pull request, so you can spot which of your
open PRs need attention before opening any of them:

```bash
bb pr list --repo MYPROJ/payments --with-review-status
```

### 5. Conduct code reviews and submit feedback

As an autonomous agent performing code reviews or addressing comments:

```bash
# Submit an approval once verified
bb pr review approve 42 --repo MYPROJ/payments

# Request changes with a summary note if issues were found
bb pr review complete 42 --repo MYPROJ/payments --status NEEDS_WORK --comment "Unit tests failed in payment_test.go"
```

### 6. Address review feedback and tasks

Bitbucket models a task as a blocker comment, so `bb pr comment list` returns
reviewer comments *and* tasks in one view, unresolved first, each with its file
anchor, resolution state and reply count.

```bash
# Everything still waiting on you — comments and tasks together
bb pr comment list --repo MYPROJ/payments 42 --unresolved
```

```
3 unresolved, 1 open task, 5 resolved

! [118] Alice A  src/main/java/com/example/PaymentService.java:42  (2 replies)
    This should handle a null customer.
! [131] Carol C  (task)
    Add a regression test for the retry path.
```

```bash
# Only the tasks that block merging
bb pr comment list --repo MYPROJ/payments 42 --tasks-only --unresolved

# Read a full thread, replies included
bb pr comment list --repo MYPROJ/payments 42 --unresolved --with-replies

# Scope to one file
bb pr comment list --repo MYPROJ/payments 42 --path src/main/java/com/example/PaymentService.java
```

Then fix and report back:

```bash
# Post a progress note on the pull request
bb pr comment add 42 --repo MYPROJ/payments --text "Fixed in <commit>. Please re-review."

# Close a task once it is addressed, or put it back if it was not
bb pr comment resolve 42 131 --repo MYPROJ/payments
bb pr comment reopen 42 131 --repo MYPROJ/payments
```

Resolving is what closing a task became: Bitbucket removed pull request tasks in
8.0 and folded them into blocker comments, so `resolve` on the comment is the
whole of it. `bb pr task *` is gone — it called the removed endpoint and could
never have worked.

Re-run the listing afterwards to confirm the thread count dropped. Note that
reviewers resolve their own threads, so an addressed comment stays unresolved
until they mark it — `action_required` reflects Bitbucket's state, not yours.

A comment carrying a fenced ` ```suggestion ` block is flagged with
`has_suggestion` and can be applied directly:

```bash
bb pr comment apply-suggestion --repo MYPROJ/payments 42 118
```

`--json` returns the same thread view without Bitbucket's nested pull request
payload. Use `--full` if you need the raw Bitbucket comment objects instead.

### 7. Merge pull requests (auto-merge or direct merge)

Auto-merge lets Bitbucket complete the merge as soon as required builds pass and
all approvals are in, instead of polling and merging manually (Bitbucket DC 8.0+).
Only enable it after review feedback is addressed and required checks are green.

```bash
# Inspect current auto-merge configuration
bb pr auto-merge get --repo MYPROJ/payments 42

# Enable auto-merge (default strategy: no-ff). Prefer a rebase strategy for linear history.
bb pr auto-merge enable --repo MYPROJ/payments 42 --strategy rebase-ff-only

# Cancel auto-merge
bb pr auto-merge disable --repo MYPROJ/payments 42

# Or merge directly once all checks are green
bb pr merge 42 --repo MYPROJ/payments
```

### 8. Diagnose a CI failure

```bash
# Get build statuses for a specific commit
bb build status get <commit-sha>

# Get commit details for context
bb commit get --repo MYPROJ/payments <commit-sha>

# Compare against the previous green commit
bb commit compare <green-sha> <failing-sha> --repo MYPROJ/payments
```

### 9. Release tagging

```bash
# Find the current latest tag
bb tag list --repo MYPROJ/payments

# Create a new release tag
bb tag create v1.2.3 --repo MYPROJ/payments --start-point main
```

### 10. File browse/edit, comparison and archives

Read or edit repository files over REST without cloning, compare refs/branches, or download repository archives:

```bash
# Print raw file contents to stdout
bb repo cat README.md --repo MYPROJ/payments --at main

# Edit/create a file directly via REST
bb repo edit README.md --repo MYPROJ/payments --branch main --message "Update README" --content "New content..."

# Compare commits or branches to list changed files
bb repo compare main feature/my-work --repo MYPROJ/payments

# Show a unified diff of changes between two refs
bb repo compare main feature/my-work --repo MYPROJ/payments --diff

# Download a repository archive (defaults to zip format)
bb repo archive --repo MYPROJ/payments --at main --output payments-main.zip

# Stream repository archive to stdout
bb repo archive --repo MYPROJ/payments --format tar.gz -o - > archive.tar.gz
```

### 11. Server-side hooks

`bb` does not manage plugin hooks or hook scripts. Both configure code that runs
inside Bitbucket on every push, and neither belongs in a CLI workflow — see
[Server-Side Hooks](advanced/server-side-hooks.md). Use `webhook` to have
Bitbucket call out to a service you control instead.

### 12. SSH keys and HTTP Access Tokens

Manage personal SSH keys or repository/project-level SSH access keys, and create/manage HTTP access tokens:

```bash
# --- SSH Keys ---
# List your personal SSH keys
bb ssh-key list

# Add a personal SSH key
bb ssh-key add ~/.ssh/id_ed25519.pub --label "My Laptop"

# List repository-level SSH access keys (use --project for project-level)
bb repo ssh-key list --repo MYPROJ/payments

# Add repository-level SSH access key with read-write permission
bb repo ssh-key add ~/.ssh/deploy_key.pub --repo MYPROJ/payments --label "CI Deploy" --read-write

# --- HTTP Access Tokens ---
# List your HTTP access tokens (defaults to user scope)
bb auth token list

# Create a project access token (Bitbucket DC 8.2+)
bb auth token create "CI Token" --project MYPROJ --permission PROJECT_READ --expiry-days 90

# Revoke an access token by ID
bb auth token revoke token-id-123
```

### 13. Scoped builds and deployments

Associate builds and deployments with specific commits, or view statistics across multiple commits (Bitbucket DC 7.4+):

```bash
# --- Repository-scoped Builds ---
# Set a build status for a commit in a specific repository
bb build set <commit-sha> --repo MYPROJ/payments --key "ci/test" --state SUCCESSFUL --url "https://ci.example.com" --name "Unit Tests"

# Get a build status by key
bb build get <commit-sha> --repo MYPROJ/payments --key "ci/test"

# Delete a repository-scoped build status
bb build delete <commit-sha> --repo MYPROJ/payments --key "ci/test"

# View build statistics summary for multiple commits
bb build status stats <commit-sha-1> <commit-sha-2> <commit-sha-3>

# --- Deployments ---
# Create or update a deployment status for a commit
bb deployment create <commit-sha> --repo MYPROJ/payments --key "prod-deploy" --display-name "Production Deploy" --state SUCCESSFUL --url "https://deploy.example.com" --env-key "prod" --env-name "Production" --deployment-sequence-number 1

# Get deployment status details
bb deployment get <commit-sha> --repo MYPROJ/payments --key "prod-deploy"

# Delete a deployment status
bb deployment delete <commit-sha> --repo MYPROJ/payments --key "prod-deploy"

# --- Code Insights Annotations ---
# Set a code insight annotation on a commit report
bb insights annotation set <commit-sha> lint a1 --repo MYPROJ/payments --message "Style violation" --severity LOW

# List annotations on a commit
bb insights annotation list <commit-sha> --repo MYPROJ/payments
```

## MCP Server

`bb` ships a built-in MCP server for IDE integration. It exposes Bitbucket operations as MCP tools that AI agents can call directly without constructing CLI arguments.

**This is optional.** Everything above works through the CLI alone, and the two
have different context costs: an MCP server advertises every tool it exposes on
connect, whereas the workflows above cost only the text you have already read.
If you need a handful of operations, the CLI is usually the cheaper choice; if
you are making many calls per session, the tools save you argument construction.
Skip this section entirely if MCP is not enabled or not permitted in your
environment.

```bash
# List all available MCP tools
bb ai mcp tools

# Start the server (used by IDE MCP clients, not invoked manually)
bb ai mcp serve

# Restrict to a read-only PAT
bb ai mcp serve --token <read-only-pat>

# Expose only specific tools
bb ai mcp serve --tools get_pull_request,list_pr_comments,get_build_status
```

### VS Code MCP configuration (`settings.json`)

```json
{
  "mcp": {
    "servers": {
      "bb": {
        "type": "stdio",
        "command": "bb",
        "args": ["ai", "mcp", "serve"]
      }
    }
  }
}
```

### Which tools you actually get

Your MCP client receives the tool list with descriptions when it connects, so
ask it rather than this document. To see the catalogue from the CLI:

```bash
bb ai mcp tools
```

The `EXPOSURE` column is the part that matters when planning:

- `SAFE` — exposed by default. Low consequence: reading anything, opening or
  updating a pull request, adding a comment, creating a tag.
- `YOLO` — **withheld** unless the server was started with
  `bb ai mcp serve --yolo` (or `--allow-writes`). Two kinds of operation are
  withheld: those that are irreversible (merging, enabling auto-merge), and
  those that influence merge gating (reporting a build status, submitting a
  review). Approving a pull request is gated for that second reason — it is the
  input a required-reviewer check consumes.

Do not plan around a `YOLO` tool without checking it is available first. If it
is not exposed, calling it fails with an unknown-tool error *after* you have
committed to that course of action.

```bash
# Just the tools a default server exposes
bb ai mcp tools --safe-only
```

`--tools` overrides the safety filter, so an operator can expose a specific
`YOLO` tool without enabling all of them — meaning the default and allowlisted
tool sets differ. Checking the connected server's advertised list is the only
reliable answer.

## Output Modes

All commands support `--json` for machine-readable output:

```bash
bb pr get --repo MYPROJ/payments 42 --json
bb tag list --repo MYPROJ/payments --json
```

### Asking what a command returns

`--describe` prints a command's output schema instead of running it. Use it rather than
inferring the payload shape from one sample run — a sample cannot tell you which fields are
optional, or that a field can be null.

```bash
bb pr get --describe          # the JSON Schema for bb pr get --json
bb pr get --describe --json   # the same, wrapped in a bb.machine envelope
```

It needs no arguments, no required flags, no configuration and no server: the schemas are
compiled into the binary, so the answer always matches the `bb` you are holding.

Check `described` before reading `schema`. Three answers are possible:

- `"described": true` — `schema` is the published contract for that command.
- `"described": false` with a reason saying no schema is published **yet** — the payload shape
  is real but not guaranteed. Parse defensively.
- `"described": false` with a reason saying the command returns no data payload — `bb api` and
  `bb ai skill show` produce a stream or a document. No schema is coming; do not wait for one.

Most commands are currently in the second group.

Success and failure both produce a `bb.machine` envelope on stdout. Which key is
present tells you which happened:

```json
{ "data": { }, "meta": { "contract": "bb.machine", "bb_version": "v4.0.0" } }
{ "error": { "kind": "not_found", "message": "…", "exit_code": 4 }, "meta": { "contract": "bb.machine", "bb_version": "v4.0.0" } }
```

There is no contract version field. The binary version is the contract version, so a breaking
change to any payload cuts a new major release. `meta.bb_version` tells you which binary wrote
the document — treat it as provenance, not as something to branch on. To pin a contract, pin
the installed version.

Check for `error` before reading `data`. `kind` is one of `authentication`,
`authorization`, `validation`, `not_found`, `conflict`, `transient`, `permanent`,
`not_implemented`, `cancelled`, `internal` — so you can tell "fix your invocation" from
"retry later" without parsing the message. `exit_code` matches the process exit status.

`cancelled` / exit `12` means somebody interrupted the command or a deadline expired. Do
not retry it automatically: for a mutating command like `bb bulk apply` that re-runs the
work the operator just stopped. Report it and wait for instruction.

A failure may carry an optional `error.details` object — a flat string map of handles you
need to act on it. `bb bulk apply` puts `operation_id` there, which `bb bulk status <id>`
takes. Read handles from those fields; never scrape them out of `error.message`.

Exactly one JSON document reaches stdout per command, so decode it as one value. Two
documents would be a bug — report it rather than working around it, because `jq` reads a
value stream and would hide it by printing a result per document and exiting 0.

A malformed invocation — unknown flag, unknown command, bad flag value, wrong number of
arguments — reports `validation` / exit `2`. Treat that as your own mistake to correct, not
something to retry. `internal` / exit `1` means the CLI or the server genuinely failed.

## Error Reporting

If `bb` behaves unexpectedly, create an issue:

```
https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/new
```

If you cannot open the URL directly, ask the user to file the issue on your behalf.
