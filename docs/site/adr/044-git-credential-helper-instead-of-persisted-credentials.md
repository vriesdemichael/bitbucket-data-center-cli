---
search:
  boost: 0.3
---

# ADR-044: Supply git credentials through a credential helper rather than persisting them

git asks bb for Bitbucket credentials at the moment it needs them, through git's credential helper protocol in `bb auth git-credential`, and bb never writes a credential into a repository's configuration. `bb auth setup-git` configures the helper for one host. It resets that host's helper list so bb is the only helper asked, refuses to replace another helper without `--force`, and writes bb's absolute path and the configuration file it answers from into the helper line, so git authenticates as the login it was set up with. The helper answers only for the host git asks about (ADR-021). When bb runs git with a credential itself, for a clone or a fetch, the credential is scoped to the host being contacted and passed in the child's environment, which only the owner of the process can read; a command line is readable by every local account. A git too old to read configuration from its environment takes it as an argument instead.

Do not set `http.extraHeader`, `credential.helper store`, or a remote URL holding a username and password to make a repository authenticate later. Scope every credential configuration to a host: a bare `credential.helper` or an unscoped `http.extraHeader` applies to every host git contacts, and offers a Bitbucket credential to unrelated remotes. When the helper cannot help, it writes nothing to stdout and exits 0, because a non-zero exit makes git treat the lookup as failed rather than fall through to another helper or a prompt; a configuration file it cannot read is said on stderr, which git shows. Write nothing but protocol fields to stdout. Keep `store` and `erase` as accepted no-ops, so git cannot write into or clear bb's keyring.

A credential in `.git/config` is plaintext on disk beside the keyring meant to hold it, travels with every copy and archive of the repository, and never rotates, so revoking the token breaks every clone with an error that reads like a bad token. An unscoped `http.extraHeader` is attached to every HTTP request git makes from the repository, so adding an unrelated remote sends it the Bitbucket token. A helper inverts the arrangement: nothing is stored, the keyring stays the one source, and revoking a token takes effect at once.

## Not chosen

- **Keep persisting http.extraHeader but scope it to the Bitbucket host**: Stops the credential reaching other hosts, but leaves a live token in plaintext in every clone, copied and archived with it, and stale the moment the token is rotated.
- **Embed credentials in the remote URL**: Worse than a config entry: the credential appears in .git/config, in the output of git remote -v, and in any error message or log line that echoes the remote.
- **Configure a bare credential.helper rather than a host-scoped one**: git consults a bare helper for every remote it talks to, so one misbehaving helper becomes a route for offering Bitbucket credentials to unrelated hosts. Host scoping makes that structurally impossible.
- **Let git store credentials through the helper's store verb**: Creates a second writer that silently diverges from what bb auth login recorded, so revoking or replacing a token in the keyring no longer changes what git uses.
- **Add bb as a helper without resetting the host's helper list first**: credential.<url>.helper is multi-valued and git consults every configured helper in order. A helper inherited from a broader scope, such as a system credential manager, would still answer first and could return a stale credential. Setting the key to an empty value before adding bb resets the list for that host, as gh auth setup-git does for the same reason.
