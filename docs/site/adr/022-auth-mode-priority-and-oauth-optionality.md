---
search:
  boost: 0.3
---

# ADR-022: Auth mode priority and token-first policy

bb authenticates with a personal access token or with a username and password, from the environment or from what `bb auth login` stored, and a token wins when both are there. bb has no OAuth login, in a browser or otherwise: Bitbucket Data Center's OAuth 2.0 provider cannot give a command-line tool a login that lasts, or one an agent or a pipeline can use. `bb auth token-url` prints the page on the host where a person creates a token, and `bb auth login --token-stdin` stores it. `bb auth status` says which auth mode is in use and where its credential came from.

Keep token and basic auth the only paths. Do not add OAuth-dependent behaviour, and answer a request for a browser login with this record. Reopen the question only when Bitbucket gains a device authorization grant, or lets a client that holds no secret renew its token.

Token and basic auth work on every instance with nothing for an administrator to set up, and work the same for a person, an agent and a pipeline. A browser login is worth having when it ends in a credential that lasts. Bitbucket Data Center has no way there. It has no device authorization grant, so a login needs a browser on the machine that runs bb and a callback address, port included, that a system administrator registered beforehand. A client that holds no secret is given an access token for one hour and can neither renew nor revoke it. The client secret cannot be handed to every developer, because the secret alone is exchanged for a token that acts without a user.

## Not chosen

- **OAuth-only authentication**: Incompatible with many local and evaluation setups, and adds onboarding friction.
- **An opt-in browser login, by authorization code with PKCE and a loopback callback**: Needs an administrator to register a client and a fixed callback port, and gives a token that lasts an hour. An agent or a pipeline cannot use it at all.
- **Hand out the client secret, so that bb can renew the token**: The secret alone is exchanged for a token that acts without a user, at the client's scope. Handing it out hands out a shared credential.
- **A browser login that creates a personal access token**: Needs the ACCOUNT_WRITE scope, which covers the whole account, and ends with the credential bb already takes. Creating that token on the page bb auth token-url prints needs no administrator.
