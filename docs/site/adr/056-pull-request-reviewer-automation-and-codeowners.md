# ADR 056: Pull request reviewer automation, default reviewers, and CODEOWNERS

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `056`
- Title: `Pull request reviewer automation, default reviewers, and CODEOWNERS`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/056-pull-request-reviewer-automation-and-codeowners.yaml`

## Decision

`bb pr create` and `bb pr review reviewer add` automate pull request reviewer assignment to achieve full behavioral parity with the Bitbucket Data Center web interface:
1. Default Reviewers by Default on Creation: `bb pr create` automatically queries repository and
   project default-reviewer conditions for the source and target branch pair and populates matching
   reviewers and expanded reviewer groups (`--default-reviewers` defaults to true). Users can opt out
   via `--no-default-reviewers` or `--default-reviewers=false`.
   Branch names are normalized to fully qualified ref IDs (`refs/heads/...`) and the repository ID is
   resolved before the condition query, because Bitbucket matches condition patterns against ref IDs
   and returns nothing for a bare branch name.

2. Native CODEOWNERS Evaluation by Default: When creating a pull request, `bb pr create` inspects the
   diff against `.bitbucket/CODEOWNERS` (or `CODEOWNERS`) from the target branch, resolving matching
   code owners (`--codeowners` defaults to true). If the repository does not use CODEOWNERS, it is
   silently skipped without error. Users can opt out via `--no-codeowners`.
   The working copy is consulted before the server only when it is a checkout of the repository being
   targeted, so a CODEOWNERS file in an unrelated checkout never leaks reviewers into another
   repository's pull request.

3. Fine-Grained Group Selection Strategies: Group references in `.bitbucket/CODEOWNERS` support
   Atlassian selection modifiers:
   - `:all` (default): Assigns all group members.
   - `:random(N)`: Selects N members randomly from the group.
   - `:least_busy(N)`: Selects N members with the fewest active reviews on open pull requests.
   Backslash escaping of spaces is honoured on both sides of a rule: in reviewer group names
   (`@backend\\ engineers:random(2)`) and in path patterns (`design\\ assets/ @design`).

4. Attaching Automated Reviewers to Open Pull Requests: `bb pr review reviewer add <id>` provides
   explicit `--default-reviewers` and `--codeowners` flags to evaluate and attach matching reviewers to
   an existing pull request after creation. Reviewers are attached one request at a time, so the command
   reports every reviewer that was added even when a later one fails.

5. Author-Aware Filtering: Pull request authors are strictly excluded from reviewer assignment prior to
   applying group selection strategies or assigning direct reviewers, preventing Bitbucket 400 rejection.
   The author is identified by the username the server reports for the authenticated session rather than
   the configured username, which may be an email address or differ in case.

6. Failure Is Never Silent: Because both automations are on by default, a lookup that fails for a reason
   other than "not configured" prints a warning on stderr and pull request creation continues; passing
   `--default-reviewers` or `--codeowners` explicitly makes the same failure fatal. A reviewer group whose
   membership cannot be read is an error, never an empty group and never a username invented from the
   group name.

## Agent Instructions

When adding pull request reviewer functionality or modifying reviewer commands, preserve the default automatic evaluation of default reviewers and CODEOWNERS on PR creation. Ensure group expansion handles both repository-level and project-level scopes, and strictly filters out the PR author. When testing CODEOWNERS, cover group strategies (:all, :random(N), :least_busy(N)), escaped space tokens in both path patterns and group names, and last-match-wins precedence rules. Register flag aliases with a flag set normalization function, never as a second flag bound to the same slice: pflag tracks "has this flag been set" per flag, so a second binding silently discards values supplied under the other spelling.

## Rationale

In the Bitbucket Data Center web interface, opening a pull request automatically pre-fills both configured default reviewers and matching code owners from `.bitbucket/CODEOWNERS`. However, the Bitbucket REST API (`POST /pull-requests`) does not evaluate CODEOWNERS or default reviewers server-side; it expects the client to provide explicit reviewer usernames. By performing client-side evaluation with opt-out flags, `bb` provides a seamless developer experience matching user expectations in the browser while maintaining full scripting control and non-interactive predictability.

## Rejected Alternatives

- `Opt-in only CODEOWNERS and default reviewers requiring explicit flags on pr create`: Forces users to remember multiple flags (`--default-reviewers --codeowners`) to match the web UI, leading to unassigned pull requests and broken review workflows when migrating from the browser to the CLI.
- `Defer CODEOWNERS evaluation entirely to server-side third-party marketplace apps`: Bitbucket Data Center 8.14+ natively introduced `.bitbucket/CODEOWNERS` for web UI reviewer suggestions. Relying on marketplace apps introduces external dependencies and fails in environments without those apps installed.
