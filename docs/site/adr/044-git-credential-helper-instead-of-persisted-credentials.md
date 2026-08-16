# ADR 044: Supply git credentials through a credential helper rather than persisting them

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `044`
- Title: `Supply git credentials through a credential helper rather than persisting them`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/044-git-credential-helper-instead-of-persisted-credentials.yaml`

## Decision

Authenticate git operations by implementing git's credential helper protocol in bb auth git-credential, configured per host by bb auth setup-git. Never write a credential into a repository's configuration. Credentials supplied to git for a single command are passed with git -c and scoped to the host being contacted, so they live only in that process's arguments. Any lookup whose result is handed to another program must resolve the host strictly, with no fallback to the configured default host.

## Agent Instructions

Do not write credentials into a repository. Specifically, do not set http.extraHeader, credential.helper store, or a remote URL containing a username and password as a way of making a clone authenticate later. If a git operation needs credentials, either pass them with git -c for that single invocation or rely on the credential helper. Scope every credential configuration to a host. A bare credential.helper or an unscoped http.extraHeader applies to every host git contacts, which turns a Bitbucket credential into one that is offered to unrelated remotes. Use config.LoadStoredAuthForHostStrict, never config.LoadStoredAuthForHost, whenever the resolved credential leaves bb. The non-strict lookup falls back to the configured default host by design so that bb's own commands work without --host; using it in an outward-facing path returns the default server's credentials for a host that was never configured. Keep the credential helper silent when it cannot help: no output and exit 0. A non-zero exit makes git treat the credential lookup as failed rather than falling through to another helper or prompting. Write nothing but protocol fields to stdout. Keep store and erase as accepted no-ops so git cannot write into or clear bb's keyring.

## Rationale

bb repo clone previously persisted an Authorization header containing a live token into every cloned repository's .git/config. That put a credential on disk in plaintext, defeating the keyring it was otherwise stored in, and an unscoped http.extraHeader is attached to every HTTP request git makes from that repository — so adding any unrelated HTTP remote caused the Bitbucket token to be sent to that host. The credential also never rotated, so revoking it broke every existing clone with an error that reads like a bad token. This was not hypothetical. The same mechanism had already written a test credential into this project's own repository configuration and broken pushes to github.com. A credential helper inverts the arrangement: git asks for a credential at the moment it needs one and nothing is stored, so the keyring stays the single source of truth and revocation takes effect immediately. The strict-lookup rule is recorded because the first implementation of the helper did not follow it, and consequently returned the Bitbucket token when git asked for github.com credentials — the precise failure the helper was written to prevent, reintroduced one layer up.

## Rejected Alternatives

- `Keep persisting http.extraHeader but scope it to the Bitbucket host`: Scoping stops the credential being offered to other hosts but leaves a live token in plaintext in every clone, where it is copied, archived and shared with the working tree, and where it goes stale the moment the token is rotated.
- `Embed credentials in the remote URL`: Worse than a config entry: the credential appears in .git/config, in the output of git remote -v, and in any error message or log line that echoes the remote.
- `Configure a bare credential.helper rather than a host-scoped one`: git consults a bare helper for every remote it talks to, so a single misbehaving helper becomes a route for offering Bitbucket credentials to unrelated hosts. Host scoping makes that structurally impossible rather than merely unlikely.
- `Let git store credentials through the helper's store verb`: Creates a second writer for credentials that silently diverges from what bb auth login recorded, so revoking or replacing a token in the keyring no longer changes what git uses.
- `Add bb as a helper without resetting the host's helper list first`: credential.<url>.helper is multi-valued and git consults every configured helper in order. A helper inherited from a broader scope, such as a system credential manager, would still answer first and could return a stale credential. Setting the key to an empty value before adding bb resets the list for that host, which is what gh auth setup-git does for the same reason.
