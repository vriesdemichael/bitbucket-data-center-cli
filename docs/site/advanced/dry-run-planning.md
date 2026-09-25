---
search:
  boost: 1.5
---

# Dry-Run Planning

`--dry-run` asks what the real run would do, without doing it. The answer is a
verdict: whether the run would go through, how far that can be trusted, and
why.

```bash
bb --dry-run pr merge 42 --repo PROJ/app
```

```text
Dry run: bb pr merge would fail (preconditions-checked)
- update PROJ/app id=42: would fail
    Requires 2 approvals; it has 1
```

Each command's help says under **Dry run** what it checks, and `bb help dry-run`
explains the answer.

## How far a verdict can be trusted

A verdict carries a **tier**: the weakest check behind it
([ADR-078](../adr/078-dry-run-confidence-is-derived-from-a-tier.md)).

| Tier | What happened |
|---|---|
| `server-validated` | Bitbucket answered this exact question, through its own dry-run endpoint or an equivalent authoritative call |
| `preconditions-checked` | Your permission and the current state were fetched, and the preconditions for the operation were checked against them |
| `predicted` | The answer was derived from partial state, or nothing was checked |

Read the tier before acting on a verdict. `server-validated` means the server
agreed; `predicted` means the real attempt may still fail.

## The verdict

Each change the run would make is an **effect**, with an outcome:

| Outcome | Meaning |
|---|---|
| `would-apply` | the change would be made |
| `no-op` | nothing needs to change |
| `would-fail` | the run would be refused; the reasons say why |

Under `--json` or `--yaml` the answer is the document's `preview` member
([ADR-096](../adr/096-the-flags-choose-the-member-and-dry-run-answers-with-a-verdict.md)):

<!-- docs-lint: envelope-shape -->
```json
{
  "preview": {
    "tier": "preconditions-checked",
    "effects": [
      {
        "action": "update",
        "target": { "repository": "PROJ/app", "id": 42 },
        "outcome": "would-fail",
        "reasons": ["Requires 2 approvals; it has 1"]
      }
    ],
    "error": { "kind": "conflict", "message": "pull request cannot be merged", "exitCode": 5 }
  },
  "meta": { "command": "pr merge", "bbVersion": "[[ bb_version_tag ]]" }
}
```

`error` is present exactly when the run would fail, and is what it would fail
with. A failure the check runs into — invalid arguments, a pull request that
does not exist, a missing permission — is a verdict too, and arrives the same
way with no effects.

## Exit codes

| Answer | Exit |
|---|---|
| Text: the run would go through | `0` |
| Text: the run would fail | the code the real run would exit with |
| `--json` or `--yaml`: any verdict | `0`, since the verdict is in the document |
| No verdict: Bitbucket did not answer | `10` |
| No verdict: interrupted | `12` |
| No verdict: a bug in `bb` | `1` |

So `bb pr merge 42 --dry-run && bb pr merge 42` stops at the check. Without a
verdict the document carries a top-level `error` instead of `preview`, so a
wrapper cannot mistake "I could not check" for "nothing would change".

## What it covers

| Command | Under `--dry-run` |
|---|---|
| Changes something on the server | Checked against Bitbucket, or predicted |
| Changes something on this machine | Predicted, and the change is not made |
| Only reads | Runs as usual; under `--json` its data is in `preview.data` |
| `bb ai mcp serve` | Does not take the flag |

A command that changes this machine — stored credentials, host aliases, the
default host, git configuration, the skill file, shell completion, a clone, a
local branch — is previewed rather than performed. Nothing is checked first, so
its verdict is `predicted`. `bb update` answers the flag itself: it reports the
release it would install and whether that release verifies, and installs
nothing. Under `--json` its one effect is replacing the binary, and
`preview.data` is that report.

`bb ai mcp serve` does not take `--dry-run`: it starts a live server, and a
session cannot be previewed. Every command in the tree is explicitly
classified, so a mutating command cannot quietly fall through unclassified
([ADR-070](../adr/070-every-command-is-explicitly-classified-for-dry-run.md)).

## Across many repositories

`--dry-run` previews **one command against one target**, so a change across many
repositories is previewed the way it is made.

Permissions, webhooks, default tasks and branch restrictions can be set once on
the project, and Bitbucket applies them to every repository in it: one
`bb project` command, and one preview. Anything Bitbucket does not scope to a
project is a loop over `bb repo list --project PROJ --json`, and the same loop
with `--dry-run` previews each repository in turn.

## See also

- [Machine Mode and Diagnostics](machine-mode-diagnostics.md) — the document these verdicts arrive in
- [ADR-096](../adr/096-the-flags-choose-the-member-and-dry-run-answers-with-a-verdict.md) — why the flags choose the member, and what a verdict exits with
- [ADR-078](../adr/078-dry-run-confidence-is-derived-from-a-tier.md) — why the tier is derived rather than declared
