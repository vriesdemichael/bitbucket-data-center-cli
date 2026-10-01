---
search:
  boost: 0.3
---

# ADR-063: Offline release signature verification and update trust policy

`bb update` can verify a release without internet access, and every setting that decides *who may vouch for a new bb binary* is administrative policy only.

`update_trusted_root` points at a Sigstore `trusted_root.json`, as `cosign trusted-root create` writes it. With it set, verification loads its trust material from that file instead of fetching it from `https://tuf-repo-cdn.sigstore.dev`, and makes no network call: signed certificate timestamps, the transparency log entry and observer timestamps are all checked against keys the file carries. The public releases verify as published, with no re-signing. `update_tuf_url` names a mirror of the Sigstore TUF repository instead, and must be an absolute https URL. Trust material is fetched with the CLI's own transport, so the configured CA bundle and client certificate apply, and over https only, redirects included. The two settings are mutually exclusive. `update_signature_identity` and `update_signature_issuer` replace the certificate SAN and OIDC issuer bb pins by default, its GitHub Actions release workflow, for an organisation that rebuilds or re-signs the release against its own Fulcio. `allow_unverified_update: true` skips signature verification: the SHA256 checksum is still checked, so corruption is caught and tampering is not, every run warns on stderr, and the JSON result reports `trust.signatureSkipped: true`.

These five settings are read from system policy only: the system configuration file, including its `policies:` and `policy:` blocks, and on Windows `HKLM\Software\Policies\bb`. None has an environment variable or a flag. `update_base_url` keeps its flag and environment forms (ADR-059), because signature verification gates whatever a mirror serves.

Never read an update trust setting from an environment variable or a command flag. Never skip the SHA256 check, including under `allow_unverified_update`. When adding a verification step to `bb update`, keep a failure to obtain trust material distinguishable from a signature that failed to verify.

A mirror alone does not make `bb update` work offline while verification fetches its trust root from the Sigstore CDN. A trust root on disk removes that last network dependency and keeps full verification, and it is deployed by the same fleet push that delivers the corporate CA bundle. Each trust setting changes which signer bb accepts for a binary it is about to execute, so honouring one from the environment would hand that decision to anyone who can set a variable in a user's shell, on the most privileged code path in the CLI. Where the bytes come from is a different question from who signed them.

## Not chosen

- **Cache the fetched Sigstore trust root on disk and reuse it when offline**: The TUF client refreshes expired metadata by design, so a cache is a timing-dependent convenience rather than a guarantee. An air-gapped host would work until the cached metadata expired and then fail, long after the change that caused it.
- **Allow BB_UPDATE_TRUSTED_ROOT as an environment variable for users without admin rights**: A trusted root names the certificate authority allowed to vouch for the release signer, so an attacker-supplied file verifies an attacker-signed binary.
- **Support plain public-key signing (update_verify_key) alongside the identity overrides**: Key-based bundles carry no certificate identity, no signed certificate timestamps and usually no transparency log entry, so accepting them relaxes three independent verification requirements rather than redirecting one. Organisations running a private Fulcio and Rekor are covered by the trusted root and signer overrides.
