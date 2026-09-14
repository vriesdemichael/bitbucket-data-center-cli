---
search:
  boost: 0.3
---

# ADR 086: bb doctor reports the configuration without loading it

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `086`
- Title: `bb doctor reports the configuration without loading it`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/086-bb-doctor-reports-the-configuration-without-loading-it.yaml`

## Decision

A top-level bb doctor checks the configuration bb would load. It reads the stored, workspace and system files each on its own, so every problem in every file is reported rather than the first: for each file its path, whether it exists and parses, every key the configuration schema rejects with its line, and the keys the schema accepts that this file's tier never reads. It reports where each effective setting comes from and what that source overrides, and, when keyring-backed storage is required, whether the OS keyring answers a read. It needs no host, makes no network call, and never carries a secret's value: a token or password is reported as configured, with where it is held. Exit 0 means there is nothing to fix. Any issue the report shows -- an invalid file, a key its file never reads, a setting a command would refuse, a required keyring that cannot be reached -- is a permanent failure, exit 1. Under --json that run writes only the failure envelope (ADR-075): the message summarises the issues, and error.details names each under a key built from its kind, its file and its key path. A run with nothing to fix writes the report. A failure of bb doctor itself is a bug, not a finding, and keeps its own kind.

## Agent Instructions

When loading changes precedence or starts reading a key, change bb doctor's resolution in internal/config/doctor.go with it; TestDiagnoseAgreesWithTheLoader compares the two. Anything bb doctor shows as wrong must fail the run and appear in error.details; do not add a finding that only prints. Do not let bb doctor print a secret, reach the network, or require a host. Point a message about a damaged configuration file at bb doctor, not at an external validator.

## Rationale

A load stops at the first file it cannot use, so repairing one was a round trip per problem. The published schema already knew every key, but reaching it needed Python, Node or an editor with YAML schema support, and an online validator uploads a file that can hold plaintext credentials. The one tool certain to be present where bb's configuration is broken is bb. The exit status is what a caller reads first, so it carries the verdict, and error.details carries what a script needs to act on it without the report.

## Rejected Alternatives

- `bb auth config check`: The configuration carries TLS, update and policy settings as well as credentials.
- `Exit 0 with the verdict in ok, as bb auth status does`: The exit status is the signal a caller reads first; 0 must mean there is nothing to fix.
