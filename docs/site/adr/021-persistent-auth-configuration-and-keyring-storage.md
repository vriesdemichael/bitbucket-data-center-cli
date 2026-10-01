---
search:
  boost: 0.3
---

# ADR-021: Persistent auth configuration and keyring storage

`bb auth login` stores a host: its profile (URL, aliases, username, auth mode and client certificate paths) in the user's configuration file, and its token or password in the OS keyring, or in the file where no keyring can hold it (ADR-047). bb keeps several hosts, one of them the default: `bb auth server list` shows them, `bb auth server use` chooses the default, and `bb auth logout` removes one. A flag or an environment variable outranks what is stored, for the one invocation it belongs to: a flag's value is passed down that call as an override and never written into the process environment, where it would replace the user's own value and outlive the call. The whole order, with the workspace and system files, is ADR-058.

A stored credential is released only to the host it was stored for: that host, an alias of it (ADR-041), or the same host stored over `http://` and reached over `https://`. One stored for `https://` is never sent over `http://`, and a host with nothing stored for it gets no credential, never the default host's. Only the user's own file and the system file decide that: a host profile in the workspace file, which arrives with a clone, gives a host its `url` and username, never makes bb release a stored credential, and its client certificate is ignored. Resolve credentials through the configuration load or `config.LoadStoredAuthForHostStrict`, which hold to this, and do not add a lookup that falls back to the default host.

Logging in once per host beats exporting variables in every shell, and the keyring keeps the secret off disk. The host a command talks to is chosen by inputs an attacker reaches: a `.env` in any parent directory, a cloned repository's `.bb/config.yaml`, a URL given to `bb api`, `--host` on an MCP server a prompt-injected agent drives. A fallback hands the user's token to whichever host they name, and a scheme they downgrade sends it across the network in the clear. A credential stored over `http://` answering over `https://` adds TLS rather than removing it, so a login made before the server had a certificate keeps working.

## Not chosen

- **Environment variables only**: Fragile across shells and sessions, and awkward with more than one host.
- **Plaintext secrets in the configuration file**: Puts every credential on disk; the file is the fallback, not the store.
- **Fall back to the default host's credential when nothing matches**: Saves naming the host, and sends the user's token wherever a file or an agent points bb.
