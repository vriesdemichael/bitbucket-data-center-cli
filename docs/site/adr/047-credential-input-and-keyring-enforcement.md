# ADR 047: Credential input paths and enforceable keyring storage

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `047`
- Title: `Credential input paths and enforceable keyring storage`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/047-credential-input-and-keyring-enforcement.yaml`

## Decision

Never require a secret to be passed as a command-line flag value. bb auth login accepts --token-stdin and --password-stdin, which read one credential from stdin, and these are the forms the README, the quickstart and the agent skill teach. The --token and --password flags remain for compatibility and warn on stderr when used. Make keyring-backed storage enforceable rather than merely preferred. --require-keyring and BB_REQUIRE_KEYRING=1 turn a keyring failure into an error instead of a silent degradation to the plaintext config fallback, and the refusal happens before any secret is written. Enforce the policy where credentials are read as well as where they are written, so a config file written before the policy was set cannot keep serving plaintext credentials. Report how the credential in use is held. bb auth status prints a credential_storage value of keyring, environment, config-file-plaintext or none, and a plaintext credential warns once per process on stderr rather than only at login.

## Agent Instructions

Do not add a flag whose value is a secret. If a command needs one, read it from stdin and add a matching --<name>-stdin flag, or take it from the environment. Warnings about credential handling go to stderr, never stdout. Under --json stdout is a machine contract, and prose there makes the envelope unparseable — tests that share one buffer for both streams hide exactly that bug. When a policy refuses an operation, refuse before writing anything. A check that errors after the secret has reached disk is worse than no check. The keyring is reached through the keyringSet/keyringGet/keyringDelete indirection in internal/config. Use it rather than calling go-keyring directly, and swap it in tests to exercise the unavailable-keyring path; go-keyring's own mock replaces a package-level provider with no way to restore it, which makes later tests in the same binary order-dependent.

## Rationale

A flag value is visible to every local user through ps and /proc/<pid>/cmdline, appears in Windows Task Manager details, is captured by process-auditing and EDR tooling, and is written to shell history. The CLI's own documentation taught that form, so it was the path most users and agents took. gh solved this with --with-token; the stdin variants here are the same idea, named after the flags they replace. The keyring fallback was already announced, but announcing is not enforcing. The keyring is unavailable on precisely the platforms this CLI targets most — headless servers, CI containers, WSL without gnome-keyring, jump boxes — so plaintext was the default path in automation rather than an edge case, and an operator who mandated keyring-backed storage had no way to require it. Enforcement at login alone would have been close to decorative. The credential outlives the login that created it, so a machine provisioned before the policy existed would keep using plaintext indefinitely; the read path is where the guarantee has to hold. Environment credentials are exempt because they never reach the config file. That also gives the documented answer for CI: supply BITBUCKET_TOKEN per invocation and do not log in at all.

## Rejected Alternatives

- `Remove --token and --password outright`: Breaks every existing script and CI job for a risk that is sometimes acceptable — a single-user workstation, an interactive shell with history disabled. A warning on stderr moves the default without breaking callers who have considered the trade-off.
- `Prompt interactively for the secret when no flag is given`: Good for humans, useless for the primary consumer. An agent or CI job has no terminal, and a prompt that blocks on a closed stdin turns a clear error into a hang. Worth adding alongside the stdin path later; it does not replace it.
- `Make --require-keyring the default now`: It is the right end state, but it turns a working login into a failure on every headless host the moment the version is upgraded. That is a breaking change and belongs in a major release with the fallback behind an explicit --allow-insecure-storage.
- `Encrypt the config-file fallback instead of refusing it`: The key would have to live beside the ciphertext or be derived from something the machine already exposes, so it obscures the secret rather than protecting it, while presenting itself as protection. Refusing, or naming the exposure plainly, is more honest than either.
- `Enforce the policy only when reading, not at login`: Would let a login appear to succeed while writing a credential the next command refuses to use, and would leave the secret on disk in the meantime. Refusing at the point of writing is both clearer and safer.
