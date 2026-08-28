# ADR 060: Mutual TLS (mTLS) client certificate authentication

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `060`
- Title: `Mutual TLS (mTLS) client certificate authentication`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/060-mutual-tls-client-certificate-authentication.yaml`

## Decision

Support Mutual TLS (mTLS) client certificate and private key configuration across environment variables (BB_CLIENT_CERT, BB_CLIENT_KEY), CLI flags (--client-cert, --client-key), and stored host configuration profiles.
The network transport (internal/transport/network) parses PEM-encoded certificate/key pairs using crypto/tls.LoadX509KeyPair and attaches them to TLSClientConfig.Certificates while preserving the host system CA pool and custom CA bundles.

## Agent Instructions

When interacting with Bitbucket instances fronted by zero-trust or mTLS authenticating reverse proxies (Envoy, NGINX, F5, Cloudflare Access), configure client certificates via BB_CLIENT_CERT and BB_CLIENT_KEY or stored profiles in ~/.config/bb/config.yaml. Ensure certificate and key are provided together; partial configurations are rejected during validation.

## Rationale

Enterprise, financial, and defense deployments frequently require hardware or PKI-backed mutual TLS client authentication before any HTTP payload reaches Bitbucket Data Center. Supporting client certificates natively at the transport layer enables seamless CLI operations within zero-trust architectures without requiring wrapper tunnels or compromising security posture.

## Rejected Alternatives

- `Requiring external stunnel or local reverse proxy loopback`: Adds significant operational overhead, extra process lifecycle management, and platform-specific setup friction.
- `PKCS#12 (.p12 / .pfx) bundle support as primary format`: PEM is standard across Go crypto and cloud native toolchains. Support can be added later if needed.
