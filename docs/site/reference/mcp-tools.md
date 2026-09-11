---
search:
  boost: 1.2
---

# MCP Tools

This page is generated from the server's tool registry by `task docs:export-mcp-tools`. Do not edit manually.

`bb ai mcp serve` registers 24 tools. 20 are available to any connected client; 4 are withheld unless the server is started with `--yolo`.

## Available by default

Exposed to every client `bb ai mcp serve` accepts. **Not all of them are read-only** — the column says which write.

| Tool | Access | What it does |
|---|---|---|
| `add_pr_comment` | writes | Add a comment to a pull request. Provide path and line to create an inline comment on a specific file line. Provide parent_id to reply to an existing comment. |
| `compare_refs` | read-only | List commits between two refs. Returns the commits reachable from 'from' but not from 'to' -- Bitbucket's direction, which is the reverse of git log base..feature. To list what a feature branch adds, pass from=feature and to=base; the git-natural order returns nothing. |
| `create_pull_request` | writes | Create a new pull request. |
| `create_tag` | writes | Create a tag on a specific commit or ref. Use for release tagging after a PR is merged. |
| `disable_auto_merge` | writes | Disable auto-merge on a pull request. The PR will no longer be merged automatically. |
| `get_build_status` | read-only | Get build/CI statuses for a specific commit. Use this to check whether CI passed before declaring a PR ready to merge. |
| `get_commit` | read-only | Get details of a specific commit including author, message, and timestamp. |
| `get_file_content` | read-only | Get the raw content of a file in a repository. |
| `get_pr_diff` | read-only | Get the diff of a pull request as unified diff text. |
| `get_pull_request` | read-only | Get pull request details including title, state, reviewer approvals, and merge status. The review_summary field reports unresolved comment threads, open tasks and reviewers who requested changes; action_required is true when the pull request is waiting on the author, and is absent when the counts it rests on were not all measured -- read counts_source to see which were. |
| `get_repository_clone_info` | read-only | Get HTTPS and SSH clone URLs for a repository. Use these URLs with git clone to check out the repository locally. |
| `list_branches` | read-only | List branches in a repository. Use to discover existing branches before creating a new one or a pull request. |
| `list_commits` | read-only | List commits in a repository branch. Use to walk history to find a good base or diagnose what changed. |
| `list_pr_comments` | read-only | List review comment threads on a pull request, unresolved first. Bitbucket models a task as a blocker comment, so this returns reviewer comments and tasks together, each with its resolution state, file anchor and reply count. Use state=open to see only what is still waiting on the author. Without path this returns the aggregate pull request comment view derived from activities. |
| `list_pull_requests` | read-only | List pull requests. Without project/repo, lists the current user's PRs across all repositories (dashboard). |
| `list_required_builds` | read-only | List required build checks that must pass before a pull request can be merged. Check this before attempting a merge to understand what CI must succeed. |
| `list_tags` | read-only | List tags in a repository. Use to find the latest release baseline or versioning information. |
| `resolve_ref` | read-only | Resolve a branch or tag name to its tip commit SHA. Use as a cheap existence check before cloning or creating a pull request. |
| `search_repositories` | read-only | Search for repositories by name, optionally filtered by project. Returns project key, slug, and display name. |
| `update_pull_request` | writes | Update a pull request's title, description, or draft state. Use draft=false to mark a draft pull request ready for review. Requires the current version from get_pull_request for optimistic locking; a stale version is rejected rather than overwriting someone else's edit. |

## Requires `--yolo`

Withheld unless the server is started with `--yolo`, because each either cannot be undone or influences whether a pull request may merge — and an agent that can do those takes part in a control it is meant to be subject to.

| Tool | What it does |
|---|---|
| `enable_auto_merge` | Enable auto-merge on a pull request. The PR will be merged automatically once all required checks pass and reviewers have approved. Requires Bitbucket DC 8.0+. |
| `merge_pull_request` | Merge a pull request. All required build checks must pass and all reviewers must have approved. |
| `set_build_status` | Report a build/CI status for a commit back to Bitbucket. Use this when running CI pipelines that should surface results in PR views. |
| `submit_pr_review` | Set review status on a pull request: approve, unapprove, or request changes (needs_work). |

## What the split means

The line is drawn by consequence, not by whether a tool writes. Opening a pull request or tagging a commit changes no branch and gates nothing, so both are available by default even though they write. Merging, enabling auto-merge, submitting a review and reporting a build status are held back: the first two are irreversible or cause a later merge, and the last two feed the checks that decide whether a merge is allowed.

See [Enterprise Hardening](../advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve) for scoping a server to a project or repository, restricting it with a read-only token, and mandating an audit trail by policy.
