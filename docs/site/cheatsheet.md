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
| Clone via browser URL | `bb repo clone https://bitbucket.example.com/projects/PROJ/repos/my-repo` | Paste web URL directly from address bar |
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
| Create a branch | `bb branch create feature/retry --start-point main --repo PROJ/my-repo` | Created directly on server |
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
| Target via browser URL | `bb pr checkout https://bitbucket.example.com/projects/PROJ/repos/my-repo/pull-requests/42` | No `--repo` flag or local clone needed |
| Target via branch name | `bb pr checkout feature/retry` | Resolves the open PR for that branch |
| Target via hash shorthand | `bb pr checkout #42` | Convenient hash notation |
| View PR diff in terminal | `bb pr diff 42` | Unified patch against target branch |
| View PR status & blockers | `bb pr get 42` | Summary of approvals, tasks, and CI checks |
| Create a pull request | `bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "Add retries"` | Opens new pull request |
| Create as draft | `bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "WIP" --draft` | Bitbucket DC 8.0+ |
| Assign reviewers | `bb pr create ... --reviewers alice,bob` | Comma-separated or repeatable |

!!! tip "Zero-Config Target & URL Resolution (`gh`-style ergonomics)"
    Commands that accept a pull request (`bb pr checkout`, `bb pr diff`, `bb pr get`, `bb pr build status`, `bb pr merge`, etc.) accept four target formats interchangeably:

    - **Full Browser URL**: `https://bitbucket.example.com/projects/PROJ/repos/my-repo/pull-requests/42` (also supports personal repositories `~username` and `/diff`, `/commits`, subpaths). `bb` automatically extracts the project, repo, and PR number so **no `--repo` flag and no prior git clone are needed**.
    - **Source Branch Name**: `feature/retry` (resolves active PR on that branch).
    - **Hash Shorthand**: `#42`.
    - **Numeric ID**: `42`.

    Likewise, `bb repo clone` directly accepts full Bitbucket repository browser URLs from your address bar.

### 5. Code Review & Feedback

| Goal | Command | Notes |
|---|---|---|
| Open PR in browser for review | `bb browse 42` | Fastest way to write inline review comments in Web UI |
| Quick terminal approval | `bb pr review approve 42 --repo PROJ/my-repo` | Submits approval once verified locally |
| List open comments & tasks | `bb pr comment list 42 --repo PROJ/my-repo --unresolved` | Shows what feedback is still pending |
| List only blocking tasks | `bb pr comment list 42 --repo PROJ/my-repo --tasks-only --unresolved` | Merge-blocking tasks |
| Add a quick PR comment | `bb pr comment add 42 --repo PROJ/my-repo --text "LGTM, verified locally"` | Posts top-level note |

### 6. Merging & CI Checks

| Goal | Command | Notes |
|---|---|---|
| Check CI builds on PR | `bb pr build status 42` | Direct status of all CI builds on the PR's source commit |
| Check CI builds on commit | `bb build status get 1a2b3c4` | Status of all CI builds on a specific commit SHA |
| Check merge blockers & state | `bb pr get 42` | Displays approvals, blocker tasks, and merge readiness |
| List required build checks | `bb build required list --repo PROJ/my-repo` | Checks mandated by repository branch permissions |
| Enable auto-merge | `bb pr auto-merge enable 42 --repo PROJ/my-repo --strategy rebase-ff-only` | Merges automatically once checks pass and approvals arrive |
| Cancel auto-merge | `bb pr auto-merge disable 42 --repo PROJ/my-repo` | Disables pending auto-merge |
| Merge immediately | `bb pr merge 42 --repo PROJ/my-repo` | Executes merge if checks pass |

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

# 4. If all tests pass, quickly approve directly from terminal:
bb pr review approve 42 --repo PROJ/my-repo

# 5. If detailed line-by-line feedback or discussion is needed, open the Web UI:
bb browse 42
```

---

### Recipe 3: Creating and Submitting a Feature Pull Request

When your branch is ready:

```bash
# Open as a draft PR while preparing documentation
bb pr create --repo PROJ/my-repo --from-ref feature/my-work --to-ref main --title "Add payment retries" --draft

# Check CI build statuses directly on the PR
bb pr build status 42

# When ready for team review, mark ready and assign reviewers
bb pr update 42 --repo PROJ/my-repo --version 3 --draft=false
```

---

### Recipe 4: Addressing Review Feedback & Tasks

When reviewers leave comments or blocker tasks on your pull request:

```bash
# 1. Check review blockers, approvals, and open item counts
bb pr get 42

# 2. View all open tasks and unresolved threads in your terminal
bb pr comment list 42 --repo PROJ/my-repo --unresolved

# 3. Open the pull request in your browser to view inline context and respond:
bb browse 42
```

---

### Recipe 5: Checking Merge Readiness & Merging Clean Changes

Check merge status and merge via auto-merge or direct merge:

```bash
# 1. Inspect merge readiness and review blockers
bb pr get 42

# 2. Check required merge checks for the repository
bb build required list --repo PROJ/my-repo

# 3. Enable auto-merge with rebase fast-forward strategy (merges when green)
bb pr auto-merge enable 42 --repo PROJ/my-repo --strategy rebase-ff-only

# Or merge directly from terminal once all checks are satisfied
bb pr merge 42 --repo PROJ/my-repo
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
