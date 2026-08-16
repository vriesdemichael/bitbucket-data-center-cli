# ADR Records

Published Architecture and Development Decision Records for this project.

This page and linked ADR pages are generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`.

- Total ADRs: `46`
- Accepted ADRs: `43`

## ADR List

- [ADR 001: Go as primary implementation language](001-go-as-primary-implementation.md) (`architecture`, `accepted`)
- [ADR 002: Taskfile for development automation](002-taskfile-for-development-automation.md) (`development`, `accepted`)
- [ADR 003: AI agents as first-class developers](003-ai-agents-as-first-class-developers.md) (`development`, `accepted`)
- [ADR 004: Live integration tests as primary correctness gate](004-live-integration-tests-as-primary-correctness-gate.md) (`development`, `accepted`)
- [ADR 005: Minimum coverage and integration testing policy](005-minimum-coverage-and-integration-testing-policy.md) (`development`, `accepted`)
- [ADR 006: Conventional commits with hook enforcement](006-conventional-commits-with-hook-enforcement.md) (`development`, `accepted`)
- [ADR 007: Manual GitHub release workflow](007-manual-github-release-workflow.md) (`development`, `superseded`)
- [ADR 008: Layered Go architecture and package boundaries](008-layered-go-architecture-and-package-boundaries.md) (`architecture`, `accepted`)
- [ADR 009: Transport policy for Bitbucket API calls](009-transport-policy-for-bitbucket-api-calls.md) (`architecture`, `accepted`)
- [ADR 010: Go model validation and schema export](010-go-model-validation-and-schema-export.md) (`architecture`, `accepted`)
- [ADR 011: Error taxonomy and CLI exit contract](011-error-taxonomy-and-cli-exit-contract.md) (`architecture`, `accepted`)
- [ADR 012: Git backend abstraction for repository operations](012-git-backend-abstraction-for-repository-operations.md) (`architecture`, `accepted`)
- [ADR 013: CLI framework and command tree standard](013-cli-framework-and-command-tree-standard.md) (`architecture`, `accepted`)
- [ADR 014: Output contract for human and machine modes](014-output-contract-for-human-and-machine-modes.md) (`architecture`, `accepted`)
- [ADR 015: Live test harness and deterministic seeding](015-live-test-harness-and-deterministic-seeding.md) (`development`, `accepted`)
- [ADR 016: Test classification and execution policy](016-test-classification-and-execution-policy.md) (`development`, `accepted`)
- [ADR 017: Undocumented behavior registry via live tests](017-undocumented-behavior-registry-via-live-tests.md) (`development`, `accepted`)
- [ADR 018: Supported Bitbucket version policy](018-supported-bitbucket-version-policy.md) (`architecture`, `superseded`)
- [ADR 019: Configuration and secret handling policy](019-configuration-and-secret-handling-policy.md) (`development`, `accepted`)
- [ADR 020: Execgit as default Git backend](020-execgit-as-default-git-backend.md) (`architecture`, `accepted`)
- [ADR 021: Persistent auth configuration and keyring storage](021-persistent-auth-configuration-and-keyring-storage.md) (`architecture`, `accepted`)
- [ADR 022: Auth mode priority and token-first policy](022-auth-mode-priority-and-oauth-optionality.md) (`architecture`, `accepted`)
- [ADR 023: Long-term planning via GitHub issues](023-long-term-planning-via-github-issues.md) (`development`, `accepted`)
- [ADR 024: Use .tmp directory for agent temporary files](024-use-tmp-directory-for-agent-temporary-files.md) (`development`, `accepted`)
- [ADR 025: Git discipline and branch management](025-git-discipline-and-branch-management.md) (`development`, `accepted`)
- [ADR 026: PR readiness and auto-merge criteria](026-pr-readiness-and-auto-merge-criteria.md) (`development`, `accepted`)
- [ADR 027: Atlassian 9.4 docs as API reference source](027-atlassian-94-docs-as-api-reference-source.md) (`architecture`, `accepted`)
- [ADR 028: OpenAPI fix registry and parity enforcement](028-openapi-fix-registry-and-parity-policy.md) (`development`, `accepted`)
- [ADR 029: Network Isolation during Unit Testing](029-network-isolation-during-unit-testing.md) (`development`, `accepted`)
- [ADR 030: Linear rebase workflow with generated coverage artifacts](030-linear-rebase-workflow-with-generated-coverage-artifacts.md) (`development`, `superseded`)
- [ADR 031: Top-level search command tree for discovery](031-top-level-search-command.md) (`architecture`, `accepted`)
- [ADR 032: Refactor CLI root into command packages](032-cli-root-modularization-into-command-packages.md) (`architecture`, `accepted`)
- [ADR 033: Automated Conventional Commit release on main](033-automated-conventional-commit-release-on-main.md) (`development`, `accepted`)
- [ADR 034: Unified dry-run planning engine for server mutating commands](034-unified-dry-run-planning-engine.md) (`architecture`, `accepted`)
- [ADR 035: Dry-run capability signaling and test strategy policy](035-dry-run-capability-and-testing-policy.md) (`development`, `accepted`)
- [ADR 036: CLI identity bb and BB namespace](036-cli-identity-bb-and-bb-namespace.md) (`architecture`, `accepted`)
- [ADR 037: Versioned docs via MkDocs Material and mike](037-versioned-docs-via-mkdocs-material-and-mike.md) (`development`, `accepted`)
- [ADR 038: bb ai subcommand as AI-first tooling namespace](038-bb-ai-subcommand-as-ai-first-tooling-namespace.md) (`architecture`, `accepted`)
- [ADR 039: Built-in MCP server with explicit host scoping and token capability restriction](039-built-in-mcp-server-with-host-scoping-and-token-restriction.md) (`architecture`, `accepted`)
- [ADR 040: Agent skill distribution via static npx packaging and dynamic CLI generation](040-agent-skill-distribution-static-npx-and-dynamic-cli.md) (`development`, `accepted`)
- [ADR 041: Host aliases and clone URL discovery for server contexts](041-host-aliases-and-clone-url-discovery-for-server-contexts.md) (`architecture`, `accepted`)
- [ADR 042: Track the newest containerisable Bitbucket version](042-track-newest-containerisable-bitbucket-version.md) (`architecture`, `accepted`)
- [ADR 043: Provision the live test instance with the Atlassian Plugin SDK](043-sdk-provisioned-licensed-test-instance.md) (`architecture`, `accepted`)
- [ADR 044: Supply git credentials through a credential helper rather than persisting them](044-git-credential-helper-instead-of-persisted-credentials.md) (`architecture`, `accepted`)
- [ADR 045: Compute coverage measurements, commit only verifiable baselines](045-coverage-measurements-are-computed-not-committed.md) (`development`, `accepted`)
- [ADR 046: Emit a JSON error envelope on the failure path](046-json-error-envelope-on-the-failure-path.md) (`architecture`, `accepted`)

