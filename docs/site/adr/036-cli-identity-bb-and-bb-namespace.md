---
search:
  boost: 0.3
---

# ADR-036: CLI identity bb and BB namespace

The command, the binary and every release artifact are named `bb`, the artifacts as `bb_<version>_<platform>`. bb's own settings are environment variables prefixed `BB_`, such as `BB_REQUEST_TIMEOUT`; the variables that name the instance, the credential and the repository are `BITBUCKET_URL`, `BITBUCKET_TOKEN` and their siblings. The configuration lives in a `bb` directory under the user configuration directory, `~/.config/bb/` on Linux, and credentials are stored in the keyring under the service name `bb`.

Write `bb` in command examples and path assumptions. Name the variable of a new setting `BB_*`. Do not add a second name for the command, a variable or the keyring service.

One name everywhere a person or a script meets bb means automation and documentation carry no aliases and no dual namespaces.
