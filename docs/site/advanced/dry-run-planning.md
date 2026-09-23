---
search:
  boost: 1.5
---

# Dry-Run Planning

`--dry-run` asks a command what it would do instead of doing it. It is a global
flag, so it works the same way on every command that changes something on the
server.

```bash
bb --dry-run project create DEMO --name "Demo Project"
```

```text
Dry-run (stateful, capability=full)
- intent=project.create action=create predictedAction=create
  note=project will be created
```

The value of a preview depends entirely on how it was reached, and `bb` tells
you which. That is the part worth reading before you rely on one.

## How much a preview is worth

Every predicted item carries a **tier** saying how the answer was arrived at,
and a **confidence** computed from that tier
([ADR-078](../adr/078-dry-run-confidence-is-derived-from-a-tier.md)).

| Tier | What happened | Confidence |
|---|---|---|
| `server-validated` | Bitbucket answered this exact question, through its own dry-run endpoint or an equivalent authoritative call | full |
| `preconditions-checked` | Your permission and the current state were both fetched, and the preconditions for the operation were evaluated against them | full |
| `predicted` | The answer was derived from partial state | partial |

An item that states no tier is treated as `predicted`. The confidence is
computed from the tier rather than declared beside it, so the two cannot
disagree — a prediction cannot claim to be certain.

Read the tier before acting on a preview. `server-validated` means the server
agreed; `predicted` means `bb` reasoned about it from what it could see, and the
real attempt may still fail.

## What a preview contains

Under `--json` each item reports:

| Field | Meaning |
|---|---|
| `intent` | what the command set out to do |
| `target` | what it would act on |
| `action` | the operation |
| `predictedAction` | what would actually happen, when that differs — `blocked`, for instance |
| `supported` | whether this command can be previewed at all |
| `tier`, `confidence` | how the answer was reached, and what it is worth |
| `requiredState` | conditions the operation depends on |
| `blockingReasons` | why it would not succeed, when it would not |

Under `--json` the same preview arrives as an envelope:

<!-- docs-lint: envelope-shape -->
```json
{
  "data": {
    "dryRun": true,
    "planningMode": "stateful",
    "capability": "full",
    "items": [
      {
        "intent": "project.create",
        "target": { "project": "DEMO", "name": "Demo Project", "description": "" },
        "action": "create",
        "predictedAction": "create",
        "supported": true,
        "reason": "project will be created",
        "tier": "preconditions-checked",
        "confidence": "full",
        "requiredState": ["project get"]
      }
    ],
    "summary": {
      "total": 1, "supported": 1, "unsupported": 0, "noOp": 0,
      "create": 1, "update": 0, "delete": 0, "unknown": 0
    }
  },
  "meta": { "bbVersion": "[[ bb_version_tag ]]" }
}
```

`planningMode` and `capability` describe the run as a whole; `tier` and
`confidence` are per item, because one command can predict several things with
different certainty. `summary` counts what would happen, which is the part a
pipeline gates on.

```bash
bb --dry-run --json pr merge 42 --repo PROJ/repo
```

A merge that cannot proceed comes back `supported: true` with
`predictedAction: blocked` and the reasons listed, which is a successful
preview of a failure rather than an error.

## What it does not cover

`--dry-run` previews **server mutations**. Commands that only change local state
— your configuration, your git remotes, files on disk — are outside its scope,
and the flag's own help says so.

A command for which a preview means nothing refuses the flag rather than
ignoring it: `bb ai mcp serve` starts a live server, and a session cannot be
previewed. Every command in the tree is explicitly classified, so a mutating
command cannot quietly fall through unclassified
([ADR-070](../adr/070-every-command-is-explicitly-classified-for-dry-run.md)).

Transient network failures during a dry run exit `10`, the same as a real run,
so a wrapper cannot mistake "I could not check" for "nothing would change".

## Across many repositories

`--dry-run` previews **one command against one target**, so a change across many
repositories is previewed the way it is made.

Permissions, webhooks, default tasks and branch restrictions can be set once on
the project, and Bitbucket applies them to every repository in it: one
`bb project` command, and one preview. Anything Bitbucket does not scope to a
project is a loop over `bb repo list --project PROJ --json`, and the same loop
with `--dry-run` previews each repository in turn.

## See also

- [Machine Mode and Diagnostics](machine-mode-diagnostics.md) — the envelope these previews arrive in
- [ADR-078](../adr/078-dry-run-confidence-is-derived-from-a-tier.md) — why confidence is derived rather than declared
