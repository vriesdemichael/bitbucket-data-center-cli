# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`bb` is a CLI for Bitbucket Server/Data Center, built with Go and Cobra. Module: `github.com/vriesdemichael/bitbucket-server-cli`. Primary target: Bitbucket DC 9.4.x.

## Build & Test

```bash
task test:unit                    # Unit tests
task test:unit:coverage           # Unit tests with coverage
task test:live                    # Live integration tests (requires Docker Bitbucket stack)
task quality:check                # Pre-merge quality gate (linear git + ADR + coverage)
go test ./internal/services/pullrequest/...  # Run a single package's tests
```

No explicit lint task — Go vet is implicit in `go test`. CI requires 85% combined coverage and 85% patch coverage.

## Architecture

Layered Go architecture (ADR 008):

```
cmd/bb/main.go                          → Entry point, version via ldflags
internal/cli/                           → Cobra commands, flag parsing, output formatting
internal/services/                      → Business logic per domain entity
internal/openapi/ + internal/transport/  → Generated API client, HTTP transport, retry
internal/models/generated/              → Generated types from Bitbucket 9.4 OpenAPI spec
```

**Command registration**: Most commands are inline in `internal/cli/` files (e.g., `insights_pr_admin_commands.go` contains `pr` commands). Newer modular commands (`cmd/ai`, `cmd/auth`, `cmd/bulk`) use dependency injection via `Dependencies` structs.

**Service layer**: Each entity has its own package under `internal/services/` with a `Service` struct accepting an HTTP client. Services use the custom `httpclient.Client` (not the generated OpenAPI client) for REST API calls with manual JSON marshaling.

**Error taxonomy**: `internal/domain/errors/` — `AppError` with `Kind` (authentication, validation, not_found, conflict, transient, permanent, not_implemented, internal) mapped to specific exit codes (2=validation, 3=auth, 4=not_found, 5=conflict, 10=transient, 11=not_implemented, 1=internal).

## Key Patterns

- **Dry-run planning**: `internal/cli/dryrun.go` registers mutating commands with intent/action metadata. `--dry-run` intercepts `RunE` handlers to preview actions without executing.
- **JSON envelope**: All `--json` output wrapped in `{"version": "v2", "data": {...}, "meta": {"contract": "bb.machine"}}` via `internal/cli/jsonoutput/`.
- **Git-native repo inference**: When `--repo` is omitted, resolves from git remotes in CWD matching Bitbucket SSH/HTTPS clone URLs.
- **Generated code**: Models and client are generated from OpenAPI spec via `oapi-codegen`. Regenerate with `task models:generate` and `task client:generate`.
- **Config layering**: Env vars → `.env` file → stored YAML → OS keyring for credentials. Supports multiple server contexts with host aliases.

## Coverage Artifact Rebase Rule

`docs/quality/coverage-report.json` and related coverage files are regenerated artifacts that will conflict on every rebase onto `main`. After rebasing, always regenerate them and amend the quality commit (see `AGENTS.md` for the exact procedure).

## Conventions

- Linear git history only (rebase, no merge commits) — enforced by CI.
- Conventional commits for PR titles (ADR 006).
- Branch naming: `fix/`, `feat/`, `chore/` prefixes.
