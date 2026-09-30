---
search:
  boost: 0.3
---

# ADR-022: Auth mode priority and token-first policy

Use a practical auth priority for runtime operations: CLI flags/env and stored credentials with token or basic auth are first-class and required for milestone delivery. bb has no OAuth login, in a browser or otherwise. Bitbucket Data Center's OAuth 2.0 provider cannot give a command-line tool a login that lasts, or one an agent or a pipeline can use. Provide a first-class helper command that prints the host-specific personal access token (PAT) creation URL so users can quickly provision token-based auth.

Implement and maintain token/basic auth as the default operational path. Do not add OAuth-dependent behavior, and answer a request for a browser login with this record. Keep onboarding optimized for token creation and login with --token-stdin. Document authentication mode in status output and troubleshooting guidance. Reopen the question only when Bitbucket gains a device authorization grant, or lets a client that holds no secret renew its token.

Token and basic auth work on every instance with nothing for an administrator to set up, and work the same for a person, an agent and a pipeline. A browser login is worth having when it ends in a credential that lasts. Bitbucket Data Center has no way there. It has no device authorization grant, so a login needs a browser on the machine that runs bb and a callback address, port included, that a system administrator registered beforehand. A client that holds no secret is given an access token for one hour and can neither renew nor revoke it. The client secret cannot be handed to every developer, because the secret alone is exchanged for a token that acts without a user.

## Not chosen

- **OAuth-only authentication**: Incompatible with many local/eval setups and increases onboarding friction.
- **An opt-in browser login, by authorization code with PKCE and a loopback callback**: Needs an administrator to register a client and a fixed callback port, and gives a token that lasts an hour. An agent or a pipeline cannot use it at all.
- **Hand out the client secret, so that bb can renew the token**: The secret alone is exchanged for a token that acts without a user, at the client's scope. Handing it out hands out a shared credential.
- **A browser login that creates a personal access token**: Needs the ACCOUNT_WRITE scope, which covers the whole account, and ends with the credential bb already takes. Creating that token on the page bb auth token-url prints needs no administrator.
