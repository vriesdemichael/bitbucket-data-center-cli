---
search:
  boost: 0.3
---

# ADR-056: Pull request reviewer automation, default reviewers, and CODEOWNERS

`bb pr create` assigns the reviewers the Bitbucket web interface would have filled in: the default reviewers whose conditions match the source and target branches, with their reviewer groups expanded, and the code owners Bitbucket reports for the change (ADR-080). Both are on by default; `--no-default-reviewers` and `--no-codeowners` turn them off. `bb pr review reviewer add <id>` takes `--default-reviewers` and `--codeowners` to do the same for an open pull request, and adds reviewers one request at a time, so it reports every reviewer it added even when a later one fails.

Default reviewers are looked up with branch names expanded to fully qualified ref IDs (refs/heads/...) and the repository IDs resolved where they can be, because Bitbucket matches condition patterns against ref IDs and answers nothing for a bare branch name. The author is left out of every list, identified by the username Bitbucket reports for the session rather than the configured one, which may be an email address or differ in case; Bitbucket rejects a pull request whose author is a reviewer. On `bb pr create`, where the lookups run without being asked for, a lookup that fails prints a warning on stderr and the pull request is created without those reviewers, and passing the flag explicitly makes the failure fatal; a server without the code-owners endpoint, or a repository without a CODEOWNERS file, assigns no code owners and warns of nothing. On `reviewer add` every failure is fatal. A reviewer group whose membership cannot be read is an error, never an empty group. `@name` among the reviewers names a group, and is taken as a username only when no group has that name.

Keep both automations on by default for `bb pr create`. Look reviewer groups up at repository and project level. Register a flag alias with the flag set's normalization function, never as a second flag bound to the same slice: pflag tracks whether a flag was set per flag, so a second binding silently discards values given under the other spelling.

Opening a pull request in the web interface fills in default reviewers and code owners, but `POST /pull-requests` evaluates neither and takes explicit usernames. bb resolves them before creating, so a pull request opened from the CLI arrives with the reviewers it would have had from the browser.

## Not chosen

- **Opt-in only CODEOWNERS and default reviewers requiring explicit flags on pr create**: Forces users to remember two flags to match the web interface, leading to unassigned pull requests when moving from the browser to the CLI.
