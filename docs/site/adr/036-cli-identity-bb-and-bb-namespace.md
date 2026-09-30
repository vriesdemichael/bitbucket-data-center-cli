---
search:
  boost: 0.3
---

# ADR-036: CLI identity bb and BB namespace

Standardize the public CLI identity on `bb` before first broad release. This includes command invocation, release artifact binary names, environment variable namespace, local config directory naming, keyring service naming, machine output contract name, and bulk workflow apiVersion identifiers.

Use `bb` for command examples and command path assumptions. Use `BB_*` for CLI/runtime environment variables. Keep defaults under `~/.config/bb/` and use keyring service name `bb`. Emit the machine envelope under --json; it carries no contract name or version (ADR-064).

The project is still pre-public-release, so this is the safest window for intentional compatibility breaks that reduce long-term migration burden. Aligning names early avoids carrying legacy aliases and dual namespaces in automation and documentation.

## Not chosen

- **Keep bbsc primary and add bb alias**: Retains naming debt and prolongs migration complexity without user benefit pre-release.
- **Keep BBSC_* environment variables while renaming command only**: Creates an inconsistent public contract and confusion for new users.
