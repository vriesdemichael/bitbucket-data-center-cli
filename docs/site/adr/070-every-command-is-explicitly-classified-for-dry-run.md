---
search:
  boost: 0.3
---

# ADR-070: Every command is explicitly classified for dry-run, and unknown means refuse

`internal/cli/dryrun.go` classifies every command in exactly one of five registries, each answering a different question: `dryRunProfiles` describes what a server mutation would do; `readOnlyCommands` names commands that change nothing on the server; `clientLocalCommands` names local commands that change nothing or honour `--dry-run` themselves; `clientLocalMutatingCommands` names commands that change this machine, which are previewed instead of run; and `commandsWithoutDryRun` names commands a preview means nothing for, such as `bb ai mcp serve`, whose live session cannot be previewed. Those do not take `--dry-run`: passing it is a validation error of the run, exit 2, and not a verdict. A command in none of the registries is `classificationUnknown`, and under `--dry-run` it does not run: it fails as internal, which is a bug in bb and no verdict (ADR-096). Fail-closed is the point: an unclassified command is one nobody decided about, and running it is the outcome `--dry-run` exists to prevent. `TestAllCommandsExhaustivelyClassifiedForDryRun` holds the registries to the command tree, and `TestCommandVerbsAgreeWithTheirDryRunClassification` checks each command's name against its registry.

Classify every new command in the same change that adds it. Pick the registry by what the command changes, on the server or on this machine, not by what its name suggests. A command that writes anything locally belongs in `clientLocalMutatingCommands` unless it honours `--dry-run` itself. Where the name and the classification genuinely disagree, add an exemption with a reason to `verbClassificationExemptions` rather than reclassifying to satisfy the check. Do not make unknown default to anything. A default is a decision nobody made.

The verb is a cross-check, not the source. It is chosen by whoever adds the command and derived from nothing, so it can disagree with the classification, and where it does a person settles which is right: resolve writes in `bb pr comment resolve` and reads in `bb ref resolve`. Read-only and client-local stay separate although the interceptor runs both, because the verb check objects only to a mutating verb in `readOnlyCommands`. That is what lets `bb update`, which honours `--dry-run` itself, keep its name without an exemption.

## Not chosen

- **Infer the classification from the verb**: Fails open on every name it does not recognise, and this CLI is full of them: merge, decline, rebase, fork, sync, watch.
- **Derive it from the HTTP method each handler reaches**: Deriving is the preference (ADR-067), but the handler is not reachable statically, and it would make the verb cross-check a tautology.
- **Default unknown to read-only, or to a generic preview**: The first runs commands nobody classified. The second prints an intent it invented, which is worse than refusing.
