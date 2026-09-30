# Release Verification

Before packaging or mirroring `bb` into internal registries (e.g. Artifactory, Nexus, internal apt/yum/winget repos), verify the authenticity and build provenance of the downloaded release artifacts. Set the target version (e.g. `[[ bb_version ]]`) in your verification environment:

## Sigstore Keyless Signature Verification
Every release publishes keyless OIDC signatures bound to the official GitHub Actions release workflow on `refs/heads/main`:

```bash
VERSION="[[ bb_version ]]"
cosign verify-blob \
  --bundle "bb_${VERSION}_linux_amd64.tar.gz.sigstore.json" \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "bb_${VERSION}_linux_amd64.tar.gz"
```

## GitHub Build Provenance Attestation
Verify that the binary was built on official GitHub-hosted runners directly from the source repository:

```bash
VERSION="[[ bb_version ]]"
gh attestation verify "bb_${VERSION}_linux_amd64.tar.gz" \
  --repo vriesdemichael/bitbucket-data-center-cli
```

## Software Bill of Materials (SPDX 2.3 SBOM)
Each archive has its own SBOM, generated from the binary inside it and named after the archive (`bb_${VERSION}_linux_amd64.spdx.json`). It lists the Go modules that binary links, the Go standard library it was linked with, and each module's licence. Dependencies differ by platform, so an SBOM describes one platform only. The `.deb` and `.rpm` install the `_noupdate` binary and are attested with its SBOM (`bb_${VERSION}_linux_amd64_noupdate.spdx.json`). Before a release is published, each SBOM is checked against the build information the Go linker wrote into its binary. Verify that the released archive is attested with its SBOM:

```bash
VERSION="[[ bb_version ]]"
gh attestation verify "bb_${VERSION}_linux_amd64.tar.gz" \
  --repo vriesdemichael/bitbucket-data-center-cli \
  --predicate-type https://spdx.dev/Document/v2.3
```

To inspect an SBOM itself, download it and verify its signature like any other artifact:

```bash
VERSION="[[ bb_version ]]"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/v${VERSION}/bb_${VERSION}_linux_amd64.spdx.json"
curl -LO "https://github.com/vriesdemichael/bitbucket-data-center-cli/releases/download/v${VERSION}/bb_${VERSION}_linux_amd64.spdx.json.sigstore.json"
cosign verify-blob \
  --bundle "bb_${VERSION}_linux_amd64.spdx.json.sigstore.json" \
  --certificate-identity 'https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "bb_${VERSION}_linux_amd64.spdx.json"
```


