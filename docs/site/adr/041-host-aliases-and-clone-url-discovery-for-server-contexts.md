---
search:
  boost: 0.3
---

# ADR-041: Host aliases and clone URL discovery for server contexts

A stored server context can carry aliases: the other host names its instance answers to, such as a separate host for SSH clones. An alias is stored on the canonical context, never inferred from a host name, and normalised to `host:port`, keeping an explicit port and taking 443, 80 or 22 for https, http or ssh when none is given. Repository inference matches a git remote against the canonical URL and every alias, and resolves it to the canonical URL for API calls; the stored-credential lookup counts an alias as the host it belongs to. An alias belongs to one context, and adding one that another context already has is a conflict.

`bb auth login` discovers aliases unless `--discover-aliases=false`, and `bb auth alias discover` does it on request. Discovery reads one small page of the repositories the user recently accessed, then one of all repositories, stops at the first repository that has SSH clone links, and derives aliases from those links alone. A login never fails because discovery did. Discovered aliases are added to the stored ones, so an alias added by hand survives.

Many deployments serve the web and API from one host name and clone traffic from another, such as bitbucket.company.org and git.company.org. Treated as unrelated, they break repository inference and credential reuse for what is one instance. Explicit aliases keep the match inspectable, and the server's own clone links cover the common case without guessing from host names.

## Not chosen

- **Match host names and ignore ports**: Conflates distinct endpoints and loses SSH port distinctions.
- **Scan every repository during discovery**: Too expensive, when one accessible repository is enough.
