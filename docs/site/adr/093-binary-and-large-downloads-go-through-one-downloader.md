---
search:
  boost: 0.3
---

# ADR 093: Binary and large downloads go through one downloader

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `093`
- Title: `Binary and large downloads go through one downloader`
- Category: `architecture`
- Status: `accepted`
- Amends: `9`
- Provenance: `guided-ai`
- Source: `docs/decisions/093-binary-and-large-downloads-go-through-one-downloader.yaml`

## Decision

A body that may be large or slow -- a release's files, a repository archive, a file's bytes, the answer to a bb api GET -- is fetched by internal/transport/download. API calls keep their clients. The request timeout bounds each wait on the server rather than the whole exchange: the connection and the response headers, then every read of the body as a stall timeout. A download that keeps sending takes as long as it needs, and the caller's context still ends it. Each call names a size cap, because without a deadline nothing else bounds what a server can make bb read; only a body written to a file or stdout, and held nowhere, goes uncapped. A body that breaks off is resumed with Range and If-Range where the server offers ranges and a strong validator, started again where the destination can take back what it holds, and reported where it cannot. A file is written beside its target and renamed into place only once complete.

## Agent Instructions

Fetch a body that is not a bounded API answer through download.Downloader -- httpclient.Download for Bitbucket -- and give it a cap unless it streams somewhere that holds nothing. Never put http.Client.Timeout on a download or read one whole with io.ReadAll. Map the StatusError it returns as the caller's API would, and pass the caller's transport in rather than replacing it, so a guard still sees every request and redirect.

## Rationale

A deadline for the whole exchange makes a download's size the thing that fails it: a release installs only over a fast enough link, and a large repository cannot be archived at all. A server that has stopped sending is what a timeout exists to catch. Resuming and starting again make a dropped connection cost a retry rather than the transfer.

## Rejected Alternatives

- `Raise request_timeout`: It loosens every API call as well, and a larger download on a slow enough link still fails.
- `A separate download-timeout setting`: One more setting to find and tune, and still a deadline for the whole transfer.
