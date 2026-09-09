---
search:
  boost: 0.3
---

# ADR 041: Host aliases and clone URL discovery for server contexts

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `041`
- Title: `Host aliases and clone URL discovery for server contexts`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/041-host-aliases-and-clone-url-discovery-for-server-contexts.yaml`

## Decision

Extend stored Bitbucket server contexts with explicit host aliases normalized as host:port endpoints. Repository inference and stored-auth lookup must match git remotes against both the canonical Bitbucket URL and configured aliases, while always resolving back to the canonical Bitbucket URL for API calls. Alias discovery is supported as a best-effort auth workflow that probes a small repository page and stops at the first accessible repository exposing clone links, deriving aliases only from server-provided clone URLs. Manual alias management remains first-class.

## Agent Instructions

Store aliases explicitly on the canonical server context rather than inferring them from hostname patterns. Normalize aliases as host:port identities, preserving explicit non-default ports and defaulting to 443/80/22 for https/http/ssh when omitted. Keep alias discovery cheap: query only a small repository page, stop at the first repository with usable clone links, and do not make login depend on discovery success. Prefer server-provided clone URLs over local git heuristics, and fail clearly if an alias is configured on more than one server context.

## Rationale

Many Bitbucket Server and Data Center deployments use different web/API and SSH hostnames, such as bitbucket.company.org for browser/API access and git.company.org for clone traffic. Treating these as unrelated breaks repository inference and credential reuse even though they refer to the same logical Bitbucket instance. Explicit aliases keep the behavior inspectable and deterministic, while clone-link discovery removes the manual setup burden in the common case without introducing brittle hostname guessing.

## Rejected Alternatives

- `Infer aliases from hostname patterns such as git.* vs bitbucket.*`: Too heuristic and prone to wrong-server matches in enterprise environments.
- `Keep hostname-only matching and ignore ports`: Conflates distinct endpoints and loses important SSH port distinctions.
- `Scan all repositories during alias discovery`: Too expensive and unnecessary when a single accessible repository is enough.
