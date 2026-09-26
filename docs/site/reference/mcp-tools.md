---
search:
  boost: 1.2
---

# MCP Tools

This page is generated from the server's tool registry by `task docs:export-mcp-tools`. Do not edit manually.

`bb ai mcp serve` registers 24 tools and exposes every one of them unless `--read-only`, `--tools`, `--exclude` or a scope withholds it. 7 ask the person to confirm a call in the MCP client before they run.

| Tool | Access | Asks | What it does |
|---|---|---|---|
| `add_pr_comment` | writes | never | Add a comment to a pull request. Provide path and line to create an inline comment on a specific file line. Provide parent_id to reply to an existing comment. |
| `compare_refs` | read-only | never | List the commits reachable from 'from' but not from 'to': what one ref has that the other lacks. For the commits a feature branch adds, pass from=feature and to=main. |
| `create_pull_request` | writes | never | Create a new pull request. |
| `create_tag` | writes | always | Create a tag on a specific commit or ref. Use for release tagging after a PR is merged. Asks the person to confirm in the client before it runs. |
| `disable_auto_merge` | writes | always | Disable auto-merge on a pull request. The PR will no longer be merged automatically. Asks the person to confirm in the client before it runs. |
| `enable_auto_merge` | writes | always | Enable auto-merge on a pull request. The PR will be merged automatically once all required checks pass and reviewers have approved. Requires Bitbucket DC 8.0+. Asks the person to confirm in the client before it runs. |
| `get_build_status` | read-only | never | Get build/CI statuses for a specific commit. Use this to check whether CI passed before declaring a PR ready to merge. |
| `get_commit` | read-only | never | Get details of a specific commit including author, message, and timestamp. |
| `get_file_content` | read-only | never | Read a file in a repository. Text comes back as a window of numbered lines: start_line and line_count choose it, and each answer says which lines it holds and where the next window starts. A Word, PowerPoint or Excel file comes back as the text extracted from it, and an archive (zip, jar, tar, tar.gz, tar.bz2) as a listing of its entries, both in the same windows. An image (PNG, JPEG, GIF, WebP, BMP, TIFF) comes back as an image, converted to PNG or JPEG when clients do not take its format, turned upright when its metadata says it was stored turned, and scaled down when it is large, with a note saying which. Audio and video come back as themselves beside a description when they are small, and as the description alone when not. A PDF or any other file is described by its type and size rather than shown, and a file over 64 MiB is described without being read. |
| `get_pr_diff` | read-only | never | Get the diff of a pull request as unified diff text. |
| `get_pull_request` | read-only | never | Get pull request details including title, state, reviewer approvals, and merge status. The review_summary field reports unresolved comment threads, open tasks and reviewers who requested changes; action_required is true when the pull request is waiting on the author, and is absent when the counts it rests on were not all measured -- read counts_source to see which were. |
| `get_repository_clone_info` | read-only | never | Get HTTPS and SSH clone URLs for a repository. Use these URLs with git clone to check out the repository locally. |
| `list_branches` | read-only | never | List branches in a repository. Use to discover existing branches before creating a new one or a pull request. |
| `list_commits` | read-only | never | List commits in a repository branch. Use to walk history to find a good base or diagnose what changed. |
| `list_pr_comments` | read-only | never | List review comment threads on a pull request, unresolved first. Bitbucket models a task as a blocker comment, so this returns reviewer comments and tasks together, each with its resolution state, file anchor and reply count. Use state=open to see only what is still waiting on the author. Without path this returns the aggregate pull request comment view derived from activities. |
| `list_pull_requests` | read-only | never | List pull requests. With project and repo, lists that repository's. Without repo, lists your own pull requests across every repository (the dashboard), narrowed to project when one is given. |
| `list_required_builds` | read-only | never | List required build checks that must pass before a pull request can be merged. Check this before attempting a merge to understand what CI must succeed. |
| `list_tags` | read-only | never | List tags in a repository. Use to find the latest release baseline or versioning information. |
| `merge_pull_request` | writes | always | Merge a pull request. All required build checks must pass and all reviewers must have approved. Asks the person to confirm in the client before it runs. |
| `resolve_ref` | read-only | never | Resolve a branch or tag name to its tip commit SHA. Use as a cheap existence check before cloning or creating a pull request. |
| `search_repositories` | read-only | never | Search for repositories by name, optionally filtered by project. Returns project key, slug, and display name. |
| `set_build_status` | writes | always | Report a build/CI status for a commit back to Bitbucket. Use this when running CI pipelines that should surface results in PR views. Asks the person to confirm in the client before it runs. |
| `submit_pr_review` | writes | always | Set review status on a pull request: approve, unapprove, or request changes (needs_work). Asks the person to confirm in the client before it runs. |
| `update_pull_request` | writes | when-draft-changes | Update a pull request's title, description, or draft state. Use draft=false to mark a draft pull request ready for review. Requires the current version from get_pull_request for optimistic locking; a stale version is rejected rather than overwriting someone else's edit. Changing draft asks the person to confirm in the client first. |

## Tools that ask

A tool asks when it merges, changes whether or when a pull request merges, or feeds a check that decides whether one may. `create_tag` asks too, since release pipelines commonly act on a new tag. `update_pull_request` asks only for a call that changes the draft flag: a draft cannot be merged, and making a pull request a draft cancels its auto-merge.

The confirmation is an MCP elicitation. The client shows what the call will do, with one box to tick, and the tool acts only once the person accepts. A client that cannot show a confirmation gets error -32021 (missing required client capability) for those tools, and nothing reaches Bitbucket. Whether a client puts the question to the person or answers it itself is the client's to decide.

## Read-only

`bb ai mcp serve --read-only` exposes only the read-only tools. It is for a client you do not trust with the tool annotations and the confirmations: a client that cannot be trusted with them should not make changes in Bitbucket, so make them yourself.

See [Enterprise Hardening](../advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve) for scoping a server to a project or repository, restricting it with a read-only token, and mandating an audit trail by policy.
