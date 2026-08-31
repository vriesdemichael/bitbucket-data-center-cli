# ADR 070: Every command is explicitly classified for dry-run, and unknown means refuse

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `070`
- Title: `Every command is explicitly classified for dry-run, and unknown means refuse`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/070-every-command-is-explicitly-classified-for-dry-run.yaml`

## Decision

internal/cli/dryrun.go classifies every command through three registries, each answering a different question: dryRunProfiles describes what a mutating command would do, readOnlyCommands names commands that change nothing on the server, and clientLocalCommands names commands that never reach it. A command matching none of them is classificationUnknown, and --dry-run refuses it rather than running it. Fail-closed is the point: an unclassified command is one nobody decided about, and running it is the outcome --dry-run exists to prevent. Two governance tests hold the registries to the command tree, so the classification cannot fall behind a newly added command.

## Agent Instructions

Classify every new command in the same change that adds it. The exhaustiveness test fails otherwise. Pick the registry by what the command does to the server, not by what its name suggests. Where the name and the classification genuinely disagree, add an exemption with a reason rather than reclassifying to satisfy the check. Do not make unknown default to anything. A default is a decision nobody made.

## Rationale

The classification was once inferred from the command's verb. That fails one-directionally -- it permits what it does not recognise -- and this CLI is full of names it could never have caught: merge, decline, rebase, fork, sync, watch. The verb still earns its keep as a cross-check rather than as the source. The name is chosen by whoever adds the command and derived from nothing, so it can disagree with the classification, and it found a real ambiguity on its first run: resolve writes in one command and reads in another. Read-only and client-local stay separate although the interceptor treats them alike, because the verb check only objects to a mutating verb in readOnlyCommands. That is what lets a client-local command be called install or checkout without an exemption.

## Rejected Alternatives

- `Keep inferring the classification from the verb`: Fails open on every name it does not recognise, which is most of them.
- `Derive it from the HTTP method each handler reaches`: Deriving is the preference (ADR-067), but the handler is not reachable statically, and it would make the verb cross-check a tautology.
- `Default unknown to read-only, or to a generic preview`: The first runs commands nobody classified. The second prints an intent it invented, which is worse than refusing.
