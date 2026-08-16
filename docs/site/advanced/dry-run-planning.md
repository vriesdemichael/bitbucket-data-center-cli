# Dry-Run Planning

## Purpose

Dry-run support provides safe previews for server-mutating commands without applying write side effects.

## Contract highlights

- Dry-run is activated with global `--dry-run`.
- Scope is server-mutating Bitbucket commands only.
- Output includes explicit planning metadata, including planning mode and capability signals.
- Live integration tests validate no-side-effect guarantees for stateful paths.

## Bulk relationship

Bulk workflows follow the same planning intent but with dedicated commands:

- Use `bb bulk plan` for preview/review.
- Use `bb bulk apply` for execution from reviewed plans.

`bb bulk apply` intentionally rejects `--dry-run` to keep reviewed-plan semantics explicit.

## Practical examples

```bash
bb --dry-run --json project create DEMO --name "Demo"
bb --dry-run repo settings security permissions users grant ci-bot REPO_WRITE --repo TEST/my-repo
```

If you need broad multi-repo planning, switch to bulk workflows documented in [Bulk Operations](bulk-operations.md).
