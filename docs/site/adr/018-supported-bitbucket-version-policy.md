# ADR 018: Supported Bitbucket version policy

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `018`
- Title: `Supported Bitbucket version policy`
- Category: `architecture`
- Status: `superseded`
- Superseded By: `042`
- Provenance: `guided-ai`
- Source: `docs/decisions/018-supported-bitbucket-version-policy.yaml`

## Decision

Support Atlassian Bitbucket 9.4.16 as the primary compatibility target initially. Additional versions may be introduced through explicit decision updates and expanded live test coverage.

## Agent Instructions

Assume 9.4.16 behavior as baseline unless a decision explicitly extends support. Version-specific handling must be documented and validated with live tests.

## Rationale

Narrowing the initial compatibility surface enables faster delivery and stronger correctness. Controlled expansion avoids accidental multi-version support with unverified behavior.

## Rejected Alternatives

- `Unbounded multi-version support from day one`: Too broad for reliable behavior validation in early phases.
