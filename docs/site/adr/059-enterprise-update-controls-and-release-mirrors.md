# ADR 059: Enterprise update controls and release mirror support

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `059`
- Title: `Enterprise update controls and release mirror support`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/059-enterprise-update-controls-and-release-mirrors.yaml`

## Decision

Provide centralized control over CLI updates for enterprise and air-gapped environments, including update disabling mechanisms and internal release mirror resolution.
1. Disabling Self-Update:
   - Runtime Policy: An administrator can disable self-updates machine-wide by setting `disable_update: true`
     in the system configuration file or the Windows registry, or by setting `BB_DISABLE_UPDATE=1`. When disabled,
     `bb update` terminates immediately with exit code 3 (`KindAuthorization`).
   - The refusal message names which of the two levers fired, and the resolved policy path when it is the policy
     file. Both are legitimate ways to administer a fleet — an environment variable reaches machines through MDM, a
     container image, a login profile, or a CI runner definition — but they live in unrelated places, and an
     operator re-enabling self-update has to know which one to go and change. A single shared sentence left them
     hunting for a variable that may be set anywhere in the login chain.
   - `update_tuf_url` is held to an absolute `https` URL. It names where Sigstore trust material comes from, and
     `url.Parse` — which accepts a bare word, a relative path, and any scheme — was never a check on it.
   - Compile-time Tag: Distributions packaged for managed OS repositories (e.g. RPM, DEB, Homebrew, WinGet)
     can compile with `-tags no_self_update` to eliminate update execution entirely, reporting:
     `self-update is disabled in this build; update bb using your system package manager`.

2. Release Mirror Resolution:
   - The CLI supports querying internal artifact mirrors (such as JFrog Artifactory, Sonatype Nexus, or internal
     release caches) instead of hardcoding `https://api.github.com`.
   - Mirror precedence: `--base-url` flag > `BB_UPDATE_BASE_URL` > Workspace Config > User Config > System Config >
     Default (`https://api.github.com`).
   - When querying custom mirrors, relative asset URLs are resolved against the configured base URL. In air-gapped
     networks where release manifests reference firewalled `github.com` URLs, the client falls back to downloading
     assets directly from `{baseURL}/{assetName}`.

## Agent Instructions

Do not allow `bb update` to execute when `BB_DISABLE_UPDATE=1`, `disable_update: true` is configured in system policy, or when compiled with `-tags no_self_update`. Always resolve release manifests and binary downloads via the configured release mirror base URL when specified.

## Rationale

Enterprise fleets governed by centralized endpoint management (e.g. SCCM, Intune, Munki, Jamf) mandate that software updates be deployed through approved packaging pipelines rather than individual user workstations invoking in-place binary self-updates. Furthermore, air-gapped and high-security enterprise enclaves block outbound access to `api.github.com` and `github.com`, requiring releases, checksums, and Sigstore verification bundles to be mirrored internally.

## Rejected Alternatives

- `Remove the update command entirely for all builds`: Standalone binary users and developer workstation environments benefit greatly from automated self-updates with Sigstore cryptographic verification. Disabling update must be opt-in per organization or package distribution.
- `Only support environment variables for mirror configuration`: Fleet administrators need system-wide configuration files to set company-wide mirrors without expecting individual developers to configure shell profiles.
