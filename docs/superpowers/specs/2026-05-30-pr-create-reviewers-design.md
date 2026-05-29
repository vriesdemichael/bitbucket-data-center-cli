# Design: `--reviewers` Flag for `bb pr create`

**Date:** 2026-05-30
**Status:** Approved

## Goal

Add an optional `--reviewers` flag to `bb pr create` so users can add one or more reviewers at PR creation time, instead of creating the PR first and then calling `pr review reviewer add` separately.

## Approach: Inline Reviewers in Create Payload

The Bitbucket Server REST API for `POST /rest/api/latest/projects/{key}/repos/{slug}/pull-requests` natively accepts a `reviewers` array in the request body. We embed reviewers directly in the create call — one HTTP request, no follow-up calls.

## Changes

### 1. Service Layer — `internal/services/pullrequest/service.go`

- Add `Reviewers []string` field to `CreateInput` (each entry is a username)
- In `buildCreatePayload`, when `input.Reviewers` is non-empty, add a `reviewers` key to the payload:
  ```go
  reviewers := make([]map[string]any, 0, len(input.Reviewers))
  for _, name := range input.Reviewers {
      if n := strings.TrimSpace(name); n != "" {
          reviewers = append(reviewers, map[string]any{
              "user": map[string]any{"name": n},
              "role": "REVIEWER",
          })
      }
  }
  if len(reviewers) > 0 {
      payload["reviewers"] = reviewers
  }
  ```
  This matches the existing pattern in `updateReviewer` and the Bitbucket API spec.

### 2. CLI Layer — `internal/cli/insights_pr_admin_commands.go`

- Add `--reviewers` flag using `string` type (comma-separated: `--reviewers alice,bob`)
- Parse the comma-separated value and pass into `CreateInput.Reviewers`
- Update the dry-run preview to include reviewers in the target map

### 3. MCP Tool — `internal/mcp/tools_pr.go`

- Add `reviewers` parameter (string, comma-separated) to the `create_pull_request` MCP tool
- Parse and pass into `CreateInput.Reviewers`

## What We Do NOT Change

- The existing `pr review reviewer add` command stays as-is (for adding reviewers to an existing PR)
- No changes to generated models or OpenAPI client
- No changes to `dryrun.go` registration (already stateful)

## Error Handling

- Invalid usernames are rejected by the Bitbucket Server API (returns 409 Conflict) — we surface the error as-is, same as existing reviewer add behavior
- Empty `--reviewers` flag is a no-op (reviewers field omitted from payload)
- Blank/whitespace-only entries in the reviewers list are silently skipped
