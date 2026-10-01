---
search:
  boost: 0.3
---

# ADR-060: Mutual TLS (mTLS) client certificate authentication

bb presents a client certificate when one is configured, taken from `--client-cert` and `--client-key`, then `BB_CLIENT_CERT` and `BB_CLIENT_KEY`, then the host's stored profile, which `bb auth login --client-cert --client-key` writes. The network transport in `internal/transport/network` loads the PEM pair with `tls.LoadX509KeyPair` into its TLS configuration, beside the system CA pool and any CA bundle added to it. The certificate and the key are given together or not at all; one without the other is refused when the configuration loads.

Use it where Bitbucket sits behind a proxy that demands a client certificate before any request reaches it, such as Envoy, NGINX, F5 or Cloudflare Access.

Built into the transport, mutual TLS works for every command with nothing else to run, where the alternative is a tunnel beside every bb process.

## Not chosen

- **Requiring an external stunnel or a local reverse proxy**: Another process to run and keep alive on every machine, set up differently per platform.
- **PKCS#12 (.p12, .pfx) bundles as the primary format**: PEM is the standard across Go's crypto and cloud-native tooling.
