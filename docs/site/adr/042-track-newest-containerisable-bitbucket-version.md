# ADR 042: Track the newest containerisable Bitbucket version

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `042`
- Title: `Track the newest containerisable Bitbucket version`
- Category: `architecture`
- Status: `accepted`
- Supersedes: `018`
- Provenance: `human`
- Source: `docs/decisions/042-track-newest-containerisable-bitbucket-version.yaml`

## Decision

Target the newest Bitbucket Data Center version that runs in this project's container stack and passes the live integration suite, rather than a version pinned in advance. A release that cannot run in the stack, or that runs but fails the suite, is not a target however recent it is. The version under test is recorded in exactly one place, the base image tag in docker/harness/Dockerfile. No other surface states a supported version.

## Agent Instructions

Do not state or assume a specific supported Bitbucket version in code, CLI output, or documentation, and do not reintroduce a default version target in configuration. To find the version under test, read the base image tag in docker/harness/Dockerfile. To move to a newer release, bump that tag and let the live suite decide whether it holds; do not assume the newest published release is usable. The harness derives the JVM, git and provisioned product version from that one tag, so there is nothing else to change alongside it. Where behavior differs between versions, record the version it was observed on next to the workaround and cover it with a live test.

## Rationale

The previous 9.4.16 pin drifted. The stack moved on while the configuration default, the auth status output, and the README still advertised 9.4.16, so the documented target described a version nothing was testing against. The intent was never to freeze on one release but to run the newest version that works, and that has a real upper bound: newer releases have failed to run in this containerised stack, so the newest published release and the newest supportable one are not the same thing. Support is therefore a property of the stack and the suite rather than a number to restate, and keeping it in the stack definition leaves one place to change on upgrade with no copies to drift.

## Rejected Alternatives

- `Bump the pinned target to the current stack version`: Drifts again at the next upgrade and recreates the duplicated copies this removes, without making the claim any better verified.
- `Always track the newest published Bitbucket release`: Newer releases have broken this containerised stack, so tracking the newest release would claim support for versions the suite cannot exercise.
- `Declare a supported version range`: Implies every version in the range is verified, which nothing in this project establishes, and the upper bound is whatever still runs in the container rather than a version chosen up front.
- `Remove the bitbucket_version_target field from machine output`: It is a required field of the bb.machine envelope, so removing it breaks consumers. Leaving it settable lets operators record a version for their own environment without the project asserting one.
