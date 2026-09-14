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

A top-level bb doctor checks the configuration bb would load. It reads the stored, workspace and system files each on its own, so every problem in every file is reported rather than the first: for each file its path, whether it exists and parses, every key the configuration schema rejects with its line, and the keys the schema accepts that this file's tier never reads. It reports where each effective setting comes from and what that source overrides, and, when keyring-backed storage is required, whether the OS keyring answers a read. It needs no host, makes no network call, and never carries a secret's value: a token or password is reported as configured, with where it is held. An invalid file bb reads is permanent, exit 1, the kind every command loading that file returns. Under --json the report is the one document and the exit status is zero, with the verdict in ok, as for bb auth status (ADR-075). An unreachable keyring and a valid key in the wrong file are findings, not failures.

## Agent Instructions

When loading changes precedence or starts reading a key, change bb doctor's resolution in internal/config/doctor.go with it; TestDiagnoseAgreesWithTheLoader compares the two. Do not let bb doctor print a secret, reach the network, or require a host. Point a message about a damaged configuration file at bb doctor, not at an external validator.

## Rationale

A load stops at the first file it cannot use, so repairing one was a round trip per problem. The published schema already knew every key, but reaching it needed Python, Node or an editor with YAML schema support, and an online validator uploads a file that can hold plaintext credentials. The one tool certain to be present where bb's configuration is broken is bb. A keyring that cannot be reached does not fail the check: BITBUCKET_TOKEN is the documented answer on a host without one, whatever the policy says about stored credentials.

## Rejected Alternatives

- `bb auth config check`: The configuration carries TLS, update and policy settings as well as credentials.
- `Exit non-zero under --json as well`: A failed run emits only the error envelope (ADR-075), which drops the report when it is most wanted.
