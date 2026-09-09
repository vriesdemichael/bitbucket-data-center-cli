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

Some commands are registered as not benefiting from a preview, `bb bulk apply`
among them. Every command in the tree is explicitly classified, so a mutating
command cannot quietly fall through unclassified
([ADR-070](../adr/070-every-command-is-explicitly-classified-for-dry-run.md)).

Transient network failures during a dry run exit `10`, the same as a real run,
so a wrapper cannot mistake "I could not check" for "nothing would change".

## Dry run or bulk

They answer different questions, and the difference is how many repositories are
involved.

`--dry-run` previews **one command against one target**. Use it before a change
you are about to make by hand.

`bb bulk` plans and applies a reviewed change across **many repositories**. Its
plan artifact is the preview, and it is reviewable, storable and re-readable in
a way a printed preview is not. `bb bulk apply` therefore rejects `--dry-run`
rather than accepting it and doing nothing: the plan already is the dry run, and
accepting the flag would suggest a second, weaker one exists.

If you are reaching for `--dry-run` in a loop over repositories, use
[Bulk Operations](bulk-operations.md) instead.

## See also

- [Bulk Operations](bulk-operations.md)
- [Machine Mode and Diagnostics](machine-mode-diagnostics.md) — the envelope these previews arrive in
- [ADR-078](../adr/078-dry-run-confidence-is-derived-from-a-tier.md) — why confidence is derived rather than declared
