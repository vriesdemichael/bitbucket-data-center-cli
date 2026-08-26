# Developer Cheatsheet & Cookbook

A scannable reference and recipe collection for developers using `bb` with Bitbucket Server and Bitbucket Data Center.

---

## Quick Reference Tables

### 1. Setup & Authentication

| Goal | Command | Notes |
|---|---|---|
| Log in with a token | `printf '%s' "$TOKEN" \| bb auth login https://bitbucket.example.com --token-stdin` | Stores securely in OS keyring |
| Check auth & connection | `bb auth status` | Validates credentials and server reachability |
| Configure git credentials | `bb auth setup-git` | One-time setup so `git push`/`git pull` authenticate via `bb` |
| View PAT creation URL | `bb auth token-url` | Prints link to create a new personal access token |
| Switch active server | `bb auth server use https://bitbucket.example.com` | When multiple Bitbucket hosts are configured |

### 2. Finding & Navigating Repositories

| Goal | Command | Notes |
|---|---|---|
| Clone a repository | `bb repo clone PROJ/my-repo` | Infers clone URL using authenticated credentials |
| Fork a repository | `bb repo fork --name my-fork --repo PROJ/my-repo` | Creates personal fork under your account |
| Search repositories | `bb search repos "payment"` | Searches across projects |
| Open repo in browser | `bb browse` | Standing in local git clone |
| Open file in browser | `bb browse src/main.go` | Resolves current branch and file path |
| Open PR in browser | `bb browse 42` | Opens pull request in browser |
| Print URL without opening | `bb browse -n` | Useful in remote SSH sessions or terminal scripts |

### 3. Branches & Commits

| Goal | Command | Notes |
|---|---|---|
| List branches | `bb branch list --repo PROJ/my-repo` | Filter with `--filter <pattern>` |
| Create a branch | `bb branch create feature/retry --from-ref main --repo PROJ/my-repo` | Created directly on server |
| Delete a branch | `bb branch delete feature/retry --repo PROJ/my-repo` | Removes branch from server |
| View commit details | `bb commit get 1a2b3c4 --repo PROJ/my-repo` | Author, message, and timestamp |
| Compare commits or refs | `bb commit compare 1a2b3c4 5d6e7f8 --repo PROJ/my-repo` | Compares two commit SHAs |
| Read file without cloning | `bb repo cat README.md --repo PROJ/my-repo --at main` | Outputs raw content to stdout |
| Diff refs on server | `bb repo compare main feature/retry --repo PROJ/my-repo --diff` | Unified diff without local git fetch |

### 4. Pull Requests: Creation & Local Testing

| Goal | Command | Notes |
|---|---|---|
| View your PR queue | `bb pr status` | Current branch PR, authored PRs, and PRs awaiting your review |
| List open PRs | `bb pr list --repo PROJ/my-repo --with-review-status` | Shows action items, blockers, and reviewer states |
| Check out a PR locally | `bb pr checkout 42` | Fetches PR branch (including forks) and switches to it |
| Check out into custom branch | `bb pr checkout 42 --branch review-42` | Avoids local branch collisions |
| View PR diff in terminal | `bb pr diff 42` | Unified patch against target branch |
| View PR status & blockers | `bb pr get 42` | Summary of approvals, tasks, and CI checks |
| Create a pull request | `bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "Add retries"` | Opens new pull request |
| Create as draft | `bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "WIP" --draft` | Bitbucket DC 8.0+ |
| Assign reviewers | `bb pr create ... --reviewers alice,bob` | Comma-separated or repeatable |

### 5. Code Review, Feedback & Tasks

| Goal | Command | Notes |
|---|---|---|
| Approve a PR | `bb pr review approve 42 --repo PROJ/my-repo` | Submits approval |
| Request changes | `bb pr review complete 42 --repo PROJ/my-repo --status NEEDS_WORK` | Marks PR as needing work |
| List open comments & tasks | `bb pr comment list 42 --repo PROJ/my-repo --unresolved` | Unresolved discussion threads |
| List only blocking tasks | `bb pr comment list 42 --repo PROJ/my-repo --tasks-only --unresolved` | Merge-blocking tasks |
| Add a comment | `bb pr comment add 42 --repo PROJ/my-repo --text "Please add a test"` | Posts top-level or reply comment |
| Apply code suggestion | `bb pr comment apply-suggestion 42 118 --repo PROJ/my-repo` | Applies reviewer's markdown suggestion block |
| Resolve a comment or task | `bb pr comment resolve 42 118 --repo PROJ/my-repo` | Marks task or thread as resolved |
| Reopen a comment or task | `bb pr comment reopen 42 118 --repo PROJ/my-repo` | Re-opens thread if feedback was incomplete |

### 6. Merging & CI Checks

| Goal | Command | Notes |
|---|---|---|
| Inspect CI build status | `bb build status get 1a2b3c4` | Status of all CI builds on commit |
| List required build checks | `bb build required list --repo PROJ/my-repo` | Checks that must pass before merging |
| Enable auto-merge | `bb pr auto-merge enable 42 --repo PROJ/my-repo --strategy rebase-ff-only` | Merges as soon as checks pass and approvals arrive |
| Cancel auto-merge | `bb pr auto-merge disable 42 --repo PROJ/my-repo` | Disables pending auto-merge |
| Merge immediately | `bb pr merge 42 --repo PROJ/my-repo --strategy rebase-ff-only` | Executes merge if checks pass |

### 7. Releases & Tags

| Goal | Command | Notes |
|---|---|---|
| List repository tags | `bb tag list --repo PROJ/my-repo` | Sorted by modification or alphabetical |
| Create release tag | `bb tag create v1.2.0 --repo PROJ/my-repo --start-point main --message "Release 1.2.0"` | Creates annotated tag on server |
| Delete tag | `bb tag delete v1.2.0 --repo PROJ/my-repo` | Deletes tag on server |
| Download archive | `bb repo archive --repo PROJ/my-repo --at v1.2.0 -o release.zip` | Downloads repo snapshot archive |

---

## The Daily Driver Cookbook (10 Real-World Recipes)

### Recipe 1: Morning Triage & Review Queue

Start your workday by inspecting your pull request inbox without switching back and forth to your browser:

```bash
# View PRs on your branch, PRs you opened, and PRs waiting for your review
bb pr status

# List open repository PRs with reviewer and task status flags
bb pr list --repo PROJ/my-repo --with-review-status
```

The output highlights `Open items` (unresolved comments, pending tasks) and reviewers who marked the PR as `needs work`.

---

### Recipe 2: Reviewing a Colleague's PR Locally

To test a colleague's changes locally, execute the test suite, and approve:

```bash
# 1. Fetch the pull request branch (including PRs from personal forks)
bb pr checkout 42

# 2. Inspect the diff in your terminal
bb pr diff 42

# 3. Check unresolved comments or open blocker tasks
bb pr comment list 42 --repo PROJ/my-repo --unresolved

# 4. Approve when satisfied
bb pr review approve 42 --repo PROJ/my-repo
```

If changes are required, mark it as needing work and add a comment:

```bash
bb pr comment add 42 --repo PROJ/my-repo --text "Tests pass, but please add retry handling for 503 responses."
bb pr review complete 42 --repo PROJ/my-repo --status NEEDS_WORK
```

---

### Recipe 3: Creating and Submitting a Feature Pull Request

When your branch is ready:

```bash
# Open as a draft PR while preparing documentation
bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "Add payment retries" --draft

# Check that CI builds passed
bb build status get 1a2b3c4

# When ready for team review, mark ready and assign reviewers
bb pr update 42 --repo PROJ/my-repo --version 3 --draft=false
```

---

### Recipe 4: Addressing Review Feedback & Tasks

When reviewers leave comments or blocker tasks on your pull request:

```bash
# 1. View all open tasks and unresolved threads
bb pr comment list 42 --repo PROJ/my-repo --unresolved

# 2. If a reviewer left a markdown code suggestion, apply it directly:
bb pr comment apply-suggestion 42 118 --repo PROJ/my-repo

# 3. Resolve addressed tasks:
bb pr comment resolve 42 118 --repo PROJ/my-repo

# 4. Confirm the review blocker count has cleared:
bb pr get 42
```

---

### Recipe 5: Auto-Merging Clean Changes

Instead of waiting for long-running CI pipelines to finish before merging manually, enable auto-merge:

```bash
# Enable auto-merge with rebase fast-forward strategy
bb pr auto-merge enable 42 --repo PROJ/my-repo --strategy rebase-ff-only
```

Bitbucket Data Center automatically merges the pull request as soon as required reviewers approve and CI checks report green.

Supported merge strategies: `no-ff`, `ff-only`, `rebase-no-ff`, `rebase-ff-only`, `squash`, `squash-ff-only`.

---

### Recipe 6: Inspecting Code Over REST (No Clone Needed)

You do not need to clone a multi-gigabyte repository just to view a file or compare branches:

```bash
# Print raw file contents to terminal or pipe to tools
bb repo cat src/config.yaml --repo PROJ/my-repo --at main | grep timeout

# View unified diff between release branches on the server
bb repo compare main feature/my-work --repo PROJ/my-repo --diff

# Open directly in your browser
bb browse src/config.yaml --repo PROJ/my-repo
```

---

### Recipe 7: One-Time Git Credential Helper Setup

`bb` authenticates to the Bitbucket REST API, but standard `git push`, `git fetch`, and `git pull` contact Bitbucket directly. To let standard `git` reuse your stored `bb` credentials:

```bash
bb auth setup-git
```

This registers `bb auth git-credential` in your global git configuration (`~/.gitconfig`). Git will now authenticate seamlessly to your Bitbucket host without prompting for passwords or storing plaintext tokens in repository configs.

---

### Recipe 8: Shell Autocompletion Setup

Generate autocompletion scripts for your shell so repository names, commands, and flags autocomplete with `<TAB>`:

=== "Bash"

    ```bash
    # Add to ~/.bashrc
    source <(bb completion bash)
    ```

=== "Zsh"

    ```zsh
    # Add to ~/.zshrc (after compinit)
    source <(bb completion zsh)
    ```

=== "Fish"

    ```fish
    # Run once to save completions
    bb completion fish > ~/.config/fish/completions/bb.fish
    ```

=== "PowerShell"

    ```powershell
    # Add to $PROFILE
    bb completion powershell | Out-String | Invoke-Expression
    ```

---

### Recipe 9: IDE MCP Server Integration (VS Code & Cursor)

`bb` includes a built-in Model Context Protocol (MCP) server that lets AI assistants (such as GitHub Copilot, Cursor, and Claude Desktop) query pull requests, CI builds, and repository files.

Add to `.vscode/settings.json` (or Cursor's MCP configuration):

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

To restrict the server to safe read-only operations:

```json
{
  "mcp": {
    "servers": {
      "bb": {
        "type": "stdio",
        "command": "bb",
        "args": ["ai", "mcp", "serve", "--token", "READ_ONLY_PAT"]
      }
    }
  }
}
```

---

### Recipe 10: Helpful Git Aliases

You can add short git aliases to make `bb` commands feel native to git:

```bash
# Run 'git pr status' or 'git pr checkout 42'
git config --global alias.pr "!bb pr"

# Run 'git browse' to open current repository in browser
git config --global alias.browse "!bb browse"
```
