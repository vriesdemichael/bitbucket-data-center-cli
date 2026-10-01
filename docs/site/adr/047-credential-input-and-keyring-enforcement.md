---
search:
  boost: 0.3
---

# ADR-047: Credential input paths and enforceable keyring storage

A secret never reaches bb as a flag value (ADR-083). `bb auth login` reads one from stdin, behind `--token-stdin` or `--password-stdin`, and stores it in the OS keyring. Where the keyring cannot hold it, the login fails before anything is written, and the error names both ways forward: `--allow-insecure-storage`, which puts the secret in the configuration file in plaintext, and `BITBUCKET_TOKEN`. A plaintext credential is announced on stderr, at login and once per process whenever it is used, and stays readable however it got there. `--require-keyring`, `BB_REQUIRE_KEYRING=1` and the `require_keyring` policy key (ADR-058) require the keyring and outrank the request: `--require-keyring` with `--allow-insecure-storage` is a usage error, and the variable and the policy refuse the plaintext the flag asks for. The requirement holds where a credential is read as well as where it is written, so a plaintext credential stored before it was set is refused rather than used. A credential from the environment is exempt, since it never reaches the file. `bb auth status` reports how the credential in use is held as `credentialStorage`: keyring, environment, config-file-plaintext or none.

A keyring entry is keyed by the host and a digest of the configuration file's canonical path (absolute, its directory's symlinks resolved, case-folded on Windows), so two configuration files keep two identities for one host. An entry keyed by the host alone answers only for a configuration file that has none of its own, and logout removes it only for the file that was using it.

Warnings about credential handling go to stderr, never stdout: under `--json` stdout is a machine contract, and prose there makes the envelope unparseable. A test that shares one buffer for both streams hides that bug. When a policy refuses an operation, refuse before writing anything; a check that fails after the secret has reached disk is worse than none. Store a secret only through `config.SaveLogin`, which holds the rule above; a command with no `--allow-insecure-storage` of its own never writes plaintext: the token prompt in `bb repo clone` uses a token the keyring cannot hold for that clone and stores it nowhere (`config.ErrSecretNotStored`). Reach the keyring through the `keyringSet`, `keyringGet` and `keyringDelete` indirection in `internal/config`, never through go-keyring directly. It gives a test binary an in-memory store, and a test swaps it to exercise an unavailable keyring, through `config.UseUnavailableKeyring` from another package; go-keyring's own mock replaces a package-level provider with no way to restore it, which makes later tests in the binary depend on their order.

The keyring is missing on the hosts bb runs on most in automation: headless servers, CI containers, WSL without a keyring service, jump boxes. A secret on disk there is one anybody with the file can use, so it is the user's decision to make, and an operator who mandates keyring storage needs a way to take that decision away. For CI, supply `BITBUCKET_TOKEN` per run and do not log in at all.

## Not chosen

- **A prompt instead of the stdin flags**: Serves a person but not an agent or a pipeline, which have no terminal, and a prompt blocking on a closed stdin turns a clear error into a hang. A prompt can sit beside the stdin path (ADR-073); it cannot replace it.
- **Fall back to plaintext with a warning**: A user lands in plaintext without deciding to, and the warning scrolls past in the one run that writes the file.
- **Encrypt the secret in the configuration file instead of refusing plaintext**: The key would have to live beside the ciphertext or come from something the machine already exposes, so it would obscure the secret while presenting itself as protection. Refusing, or naming the exposure plainly, is more honest.
- **Enforce the requirement only when reading, not at login**: A login would appear to succeed while writing a credential the next command refuses, and leave the secret on disk meanwhile.
