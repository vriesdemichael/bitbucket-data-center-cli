---
search:
  boost: 0.3
---

# ADR-059: Enterprise update controls and release mirror support

An administrator can switch `bb update` off, and point it at an internal release mirror.

`disable_update: true` in system policy -- the system configuration file or the Windows registry -- or `BB_DISABLE_UPDATE=1` makes `bb update` refuse at once with exit code 3 (authorization). The refusal names which of the two fired and, for policy, where it was set, so the operator re-enabling it knows what to change. A build with `-tags no_self_update` refuses the same way and says to update through the package manager. Every release publishes such a build of each archive, named `_noupdate`, and every package bb is published to installs it: the .deb and .rpm, Homebrew, Scoop and WinGet.

The release comes from a mirror instead of `https://api.github.com` when one is configured: `--base-url`, then `BB_UPDATE_BASE_URL`, then the workspace, user and system configuration, in that order. A relative asset URL resolves under the base URL as a directory. An asset URL off the mirror, such as the github.com address a manifest copied from GitHub names, is fetched from `{baseURL}/{assetName}` and never from its own address. `bb update` sends no credentials beyond a TLS client certificate, so a mirror serves the release anonymously; an estate whose artifact server requires a login for every download installs bb itself, with the `_noupdate` builds.

Every update URL is https by default: the base URL, each asset URL and every redirect. Plain HTTP needs `bb update --allow-http` or `BB_ALLOW_HTTP_UPDATE`, and warns on every run. A user's and a workspace's configuration have no key for it, because a workspace file arrives with a cloned repository. `allow_http_update` in system policy decides for every user when it is set: `false` refuses the opt-in and any `http://` update URL with exit code 3, and `true` permits plain HTTP without one.

Hold every request the updater sends, redirects included, to the plain-HTTP permission rather than checking the base URL alone.

A fleet under central endpoint management deploys software through its own packaging, not through each workstation replacing its binary, and an air-gapped enclave cannot reach api.github.com or github.com, so releases, checksums and Sigstore bundles are mirrored inside it. Who may sign what a mirror serves is ADR-063.

## Not chosen

- **Remove the update command entirely for all builds**: Standalone binary users benefit from self-update with Sigstore verification. Switching it off is the choice of an organisation or a package.
- **Only support environment variables for mirror configuration**: Fleet administrators need system-wide configuration files to set company-wide mirrors without expecting individual developers to configure shell profiles.
- **Gate major-version self-updates behind an extra confirmation**: Every update is a command somebody ran, package managers cross majors without asking, and the enterprise control that matters, switching update off, already exists.
