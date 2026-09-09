---
search:
  boost: 0.3
---

# ADR 024: Use .tmp directory for agent temporary files

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `024`
- Title: `Use .tmp directory for agent temporary files`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/024-use-tmp-directory-for-agent-temporary-files.yaml`

## Decision

All AI agents use the .tmp/ directory at project root for temporary files, scratch work, and intermediate outputs. Agents do not use system temp paths or locations outside the repository root for temp artifacts.

## Agent Instructions

Write temporary files (downloads, response dumps, generated scratch outputs, intermediate artifacts) to .tmp/ only. Create .tmp/ if missing. Do not use /tmp, %TEMP%, Desktop, or repository-tracked source directories. Clean up .tmp contents when no longer needed, but keep .tmp/.gitkeep.

## Rationale

Project-local temp storage avoids sandbox and permission issues with system temp locations, keeps debugging artifacts discoverable, and prevents accidental commits via .gitignore.
