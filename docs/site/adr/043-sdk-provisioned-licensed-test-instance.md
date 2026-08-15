# ADR 043: Provision the live test instance with the Atlassian Plugin SDK

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `043`
- Title: `Provision the live test instance with the Atlassian Plugin SDK`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/043-sdk-provisioned-licensed-test-instance.yaml`

## Decision

Provision the Bitbucket instance for the live integration suite with the Atlassian Plugin SDK (atlas-run), which resolves the product from Atlassian's public Maven repository and installs a development licence itself. The stack requires no licence key, no .env file and no Atlassian account, so it runs identically on a fork, in any contributor's environment, and in CI. The harness image is built from the official Bitbucket product image, so the JVM and git versions are inherited from the product rather than chosen independently.

## Agent Instructions

Do not add a BITBUCKET_LICENSE_KEY, a licence secret, or any credential to the stack definition or to CI: needing one is the failure this removes. Do not gate the live-tests job on the pull request originating from this repository, and do not let ci-complete accept a skipped live-tests result, because a skip now means the primary correctness gate silently did not run. Do not replace the harness base image with a plain JDK image. Java and git must both fall inside windows the product accepts, those windows are narrow and version-specific, and a rejected git produces an instance that logs a clean startup and only then parks in ERROR. The SDK licence is valid for three hours from process start and docker compose up -d reuses a running container, so any change to the stack must keep the healthcheck's licence-age condition; without it a stale instance still reports RUNNING and the suite fails as though the product were broken.

## Rationale

The live suite is this project's primary correctness gate, but it previously required a licensed Bitbucket instance. That had two costs. Externally, the live-tests job was skipped for pull requests from forks because the licence secret was unavailable, so a contributor's change received no signal from the gate the project relies on and could not be validated locally either. That is a hard ceiling on gaining contributors. Internally, keeping a long-lived instance running against a time-limited licence invited working around the licence rather than obtaining one. The SDK issues a development licence for exactly this purpose, which removes the secret, removes the fork restriction, and removes the incentive that created the problem.

## Rejected Alternatives

- `Keep the licensed container stack and a repository licence secret`: Fork pull requests cannot access the secret, so the primary correctness gate cannot run on external contributions and contributors cannot reproduce it locally.
- `Record and replay HTTP fixtures instead of running a real instance`: Fixtures only cover interactions already recorded. Any new command or changed call sequence needs a new recording, which needs a live instance, and a replayed capture freezes current behaviour rather than discovering server behaviour, which is what the live suite exists for.
- `Build the harness on a plain JDK image with a pinned git`: Three independently chosen versions that must stay inside narrow, version-specific windows. Bitbucket 10.4.2 requires Java 21, requires git >= 2.42, rejects >= 2.55, and additionally rejects 2.48, 2.51, 2.52 and 2.53. Inheriting both from the product image makes them correct by construction and leaves one Dependabot-managed pin.
- `Run the SDK instance against Postgres to match production deployments`: The SDK supplies an embedded database and the live suite exercises the REST API, where behaviour is equivalent. Anything characterising database-specific behaviour should stand up its own instance and say so explicitly.
