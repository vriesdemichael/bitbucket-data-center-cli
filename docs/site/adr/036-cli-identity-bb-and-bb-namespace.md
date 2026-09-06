# ADR 036: CLI identity bb and BB namespace

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `036`
- Title: `CLI identity bb and BB namespace`
- Category: `architecture`
- Status: `accepted`
- Provenance: `human`
- Source: `docs/decisions/036-cli-identity-bb-and-bb-namespace.yaml`

## Decision

Standardize the public CLI identity on `bb` before first broad release. This includes command invocation, release artifact binary names, environment variable namespace, local config directory naming, keyring service naming, machine output contract name, and bulk workflow apiVersion identifiers.

## Agent Instructions

Use `bb` for command examples and command path assumptions. Use `BB_*` for CLI/runtime environment variables. Keep defaults under `~/.config/bb/` and use keyring service name `bb`. Emit the machine envelope under --json; it carries no contract name or version (ADR-064). Use `bb.io/v1alpha1` for bulk workflow apiVersion constants and schema examples.

## Rationale

The project is still pre-public-release, so this is the safest window for intentional compatibility breaks that reduce long-term migration burden. Aligning names early avoids carrying legacy aliases and dual namespaces in automation and documentation.

## Rejected Alternatives

- `Keep bbsc primary and add bb alias`: Retains naming debt and prolongs migration complexity without user benefit pre-release.
- `Keep BBSC_* environment variables while renaming command only`: Creates an inconsistent public contract and confusion for new users.
