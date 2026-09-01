---
name: bb-bulk
description: Plan, review, and execute multi-repository bulk governance policies, declarative repository configurations, permissions, webhooks, PR settings, branch settings, and staged rollouts across Bitbucket Data Center repositories using bb.
---

# bb-bulk — Multi-Repository Bulk Governance Skill

`bb bulk` is the multi-repository policy and governance engine for Bitbucket Server and Bitbucket Data Center. Use it to declare, validate, simulate, review, and apply repository configurations, access controls, quality gates, and branch policies across multiple repositories.

> **Authoritative Command Surface**: Built-in CLI help (`bb bulk --help`, `bb bulk plan --help`, `bb bulk apply --help`, `bb bulk status --help`) reflects the exact runtime command tree. Prefer `--help` when a command option or argument signature needs runtime verification.

## The Declarative Policy Model

Bulk operations in `bb` are driven by declarative policy files (`YAML` or `JSON`). A policy specifies **which repositories to target** (`selector`) and **what actions to take** (`operations`).

### 1. IDE & Schema Validation

Always include the JSON Schema validation header at the top of your YAML policy file. This enables immediate autocomplete, syntax highlighting, and inline validation in editors (VS Code, Cursor, IntelliJ, etc.):

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PROJ
  repoPattern: "service-*"
operations:
  - type: repo.pull-request-settings.required-all-tasks-complete
    requiredAllTasksComplete: true
```

### 2. Target Selectors

The `selector` block controls how repositories are discovered:

- **Project-wide targeting**: Target all repositories inside a project:
  ```yaml
  selector:
    projectKey: INFRA
  ```
- **Pattern matching**: Target repositories within a project matching a glob pattern:
  ```yaml
  selector:
    projectKey: PROJ
    repoPattern: "microservice-*"
  ```
- **Explicit repository lists**: Target specific repositories across one or more projects:
  ```yaml
  selector:
    repositories:
      - PROJ/auth-service
      - PROJ/payment-gateway
      - CORE/common-lib
  ```
- **Combined project and explicit list**:
  ```yaml
  selector:
    projectKey: PROJ
    repositories:
      - special-service # resolves to PROJ/special-service
      - OTHER/shared-tool
  ```

### 3. Supported Operation Types

`bb bulk` supports the following declarative operation types:

| Operation Type | Description | Key Fields |
|---|---|---|
| `repo.permission.user.grant` | Grant repository-level permission to a user | `username`, `permission` (`REPO_READ`, `REPO_WRITE`, `REPO_ADMIN`) |
| `repo.permission.group.grant` | Grant repository-level permission to a group | `group`, `permission` (`REPO_READ`, `REPO_WRITE`, `REPO_ADMIN`) |
| `repo.webhook.create` | Register a webhook endpoint | `name`, `url`, `events` (e.g. `["repo:refs_changed", "pr:opened"]`), `active` (optional bool) |
| `repo.pull-request-settings.required-all-tasks-complete` | Enforce all tasks must be completed before PR merge | `requiredAllTasksComplete` (`true` or `false`) |
| `repo.pull-request-settings.required-approvers-count` | Enforce minimum number of approvals before PR merge | `count` (integer >= 0) |
| `build.required.create` | Enforce required CI build checks on target refs | `payload` (`buildParentKeys`, `refMatcher: {id: "refs/heads/master"}`) |
| `repo.settings.auto-merge` | Configure repository auto-merge behavior | `enabled` (`true` or `false`) |
| `repo.settings.auto-decline` | Configure automatic decline for stale pull requests | `enabled` (`true` or `false`), `inactivityWeeks` (integer > 0) |
| `repo.default-task.create` | Create a default PR task | `description`, `sourceRef` (optional), `targetRef` (optional) |

### Complete Policy Example

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PAYMENT
  repoPattern: "svc-*"
  repositories:
    - PAYMENT/core-gateway
operations:
  - type: repo.permission.group.grant
    group: security-auditors
    permission: REPO_READ

  - type: repo.permission.user.grant
    username: ci-deployer
    permission: REPO_WRITE

  - type: repo.pull-request-settings.required-all-tasks-complete
    requiredAllTasksComplete: true

  - type: repo.pull-request-settings.required-approvers-count
    count: 2

  - type: repo.settings.auto-merge
    enabled: true

  - type: build.required.create
    payload:
      buildParentKeys:
        - ci/pipeline
      refMatcher:
        id: refs/heads/main
```

## Multi-Stage Deterministic Workflow & Dry-Run Semantics (ADR 034)

Bulk operations follow a strict two-stage planning and execution lifecycle:

```
  ┌──────────────────────┐
  │  bulk-policy.yaml    │
  └──────────┬───────────┘
             │  bb bulk plan -f policy.yaml -o .tmp/plan.json
             ▼
  ┌───────────────────────────────────────────────────────────┐
  │  Reviewed Plan Artifact (.tmp/plan.json)                  │
  │  - Zero server writes performed                           │
  │  - Live server state checked & target repos resolved      │
  │  - Deterministic planHash generated                       │
  └──────────┬────────────────────────────────────────────────┘
             │  (Human / CI peer review and sign-off)
             │  bb bulk apply --from-plan .tmp/plan.json
             ▼
  ┌───────────────────────────────────────────────────────────┐
  │  Execution & Staged Rollout                               │
  │  - Exact reviewed plan executed                           │
  │  - Isolated per-target failures                           │
  │  - Persisted status artifact (bb bulk status <id>)        │
  └───────────────────────────────────────────────────────────┘
```

### Stage 1: Plan Generation (Zero Server Writes)

The `bb bulk plan` command queries Bitbucket Server to resolve selectors against current state and simulates all operations without writing any changes:

```bash
bb bulk plan -f policy.yaml -o .tmp/bulk-plan.json
```

What `bb bulk plan` guarantees:
1. **Zero Server Writes**: Bitbucket state is never modified during planning.
2. **Selector Resolution**: Target repositories are queried, filtered, and sorted deterministically.
3. **Preflight Validation**: Validates that users, groups, and referenced projects exist and policies are structurally correct.
4. **Deterministic `planHash`**: Generates a cryptographic SHA-256 hash representing the exact set of resolved targets and operations.

To inspect the plan in machine-readable JSON:
```bash
bb bulk plan -f policy.yaml -o .tmp/bulk-plan.json --json
```

### Validating Plan Artifacts Against JSON Schema

`bb` publishes a formal JSON Schema for bulk plans:
`https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-plan.schema.json`

Plan artifacts can be validated in two complementary ways:

1. **Automatic Built-in Verification**:
   `bb bulk apply` automatically compiles and validates the plan against `PlanJSONSchema()` and verifies the integrity of `planHash` before executing any API calls against Bitbucket:
   - Fails with a `validation` error if `apiVersion` is not `bb.io/v1alpha1`, `kind` is not `BulkPlan`, or schema fields are malformed.
   - Fails if the SHA-256 `planHash` calculated over the plan payload does not match the embedded `planHash` (preventing manual tampering or corrupted artifacts).

2. **Pre-execution Verification via CLI / Schema Validators**:
   You can validate the generated `.tmp/bulk-plan.json` against the schema in CI or pre-commit hooks using standard JSON Schema validation tools:
   ```bash
   # Using check-jsonschema:
   check-jsonschema --schemafile docs/reference/schemas/bulk-plan.schema.json .tmp/bulk-plan.json

   # Or using ajv-cli:
   npx ajv-cli validate -s docs/reference/schemas/bulk-plan.schema.json -d .tmp/bulk-plan.json
   ```

   Key required fields in the plan schema:
   - `apiVersion`: `"bb.io/v1alpha1"`
   - `kind`: `"BulkPlan"`
   - `planHash`: `sha256:<64 hex characters>`
   - `policy`: normalized policy object
   - `validation`: `{ "valid": true, "errors": [] }`
   - `summary`: `{ "targetCount": <int>, "operationCount": <int> }`
   - `targets`: non-empty array of `{ "repository": {"projectKey": "...", "slug": "..."}, "validation": {...}, "operations": [...] }`

### Why `bulk apply` Rejects `--dry-run` (ADR 034)

In single-resource commands (such as `bb repo create`), `--dry-run` performs an in-line simulation.

In bulk governance workflows, **`bb bulk plan` is the preview contract**. `bb bulk apply` consumes a reviewed, hash-verified plan file (`--from-plan`) rather than taking ad-hoc flags or running separate dry-runs. Accepting `--dry-run` on `apply` would be redundant and would bypass the reviewed artifact guarantee.

### Stage 2: Execution via Reviewed Plan

Once the plan artifact has been inspected and approved, apply it:

```bash
bb bulk apply --from-plan .tmp/bulk-plan.json
```

What `bb bulk apply` guarantees:
1. **Hash Verification**: Recomputes the plan hash and rejects the plan if it was tampered with or modified.
2. **Error Isolation per Target**: If an operation fails on one repository, remaining operations on *that repository* are skipped, but execution proceeds to the next repository.
3. **Status Persistence**: Generates a unique `operationId` and stores execution results in the local status store.

### Stage 3: Inspecting Operation Status

If an execution encountered failures or needs verification:

```bash
bb bulk status <operation-id>
```

Or as JSON for scripting and automation:
```bash
bb bulk status <operation-id> --json
```

## Structure of Status Files and Operation Artifacts

Every `bb bulk apply` execution creates a structured status artifact saved locally (in `BB_BULK_STATUS_DIR` or adjacent to configuration).

### Status Schema Breakdown

When queried via `bb bulk status <operation-id> --json`, the response `data` object contains:

- `operationId`: Unique identifier (e.g. `op-01h8q...`).
- `planHash`: Hash of the plan that was executed.
- `status`: Overall execution state:
  - `success`: All targets and operations completed successfully.
  - `partial_failure`: Some targets succeeded, while one or more targets encountered failures.
  - `failed`: All targets failed, or a fatal error halted execution.
  - `cancelled`: The run was interrupted (Ctrl-C, or an expired deadline) before every
    repository was attempted. Repositories the run never reached are recorded as
    `cancelled` rather than `failed`, so the artifact still says what was applied.
    The command exits `12`. Do not re-run it unattended: read the artifact first and
    plan from what was actually applied.
- `summary`:
  - `targetCount`: Total number of repositories targeted.
  - `operationCount`: Total operations across all targets.
  - `successfulTargets`: Count of repositories where every operation succeeded.
  - `failedTargets`: Count of repositories with at least one failure.
  - `successfulOperations`: Operations successfully applied.
  - `failedOperations`: Operations that produced an error.
  - `skippedOperations`: Operations skipped on a target because an earlier operation on that same repository failed.
  - `cancelledTargets`, `cancelledOperations`: Work an interrupted run never reached.
    Absent when the run was not interrupted. Kept apart from failed and skipped so you
    can tell "never attempted" from "tried and did not work".
- `targets`: List of target repository results:
  - `repository`: `{ "projectKey": "PROJ", "slug": "repo-name" }`
  - `status`: `success`, `failed`, or `cancelled`
  - `operations`: List of per-operation results:
    - `type`: Operation name (e.g. `repo.permission.user.grant`).
    - `status`: `success`, `failed`, `skipped`, or `cancelled`.
    - `output`: Returned API payload on success.
    - `error`: Error message on failure.
    - `errorKind`: Machine-readable error kind (e.g. `authorization`, `conflict`, `not_found`, `validation`).

### Example Partial Failure Status Artifact

```json
{
  "operationId": "op-98765",
  "planHash": "sha256:7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069",
  "status": "partial_failure",
  "summary": {
    "targetCount": 2,
    "operationCount": 4,
    "successfulTargets": 1,
    "failedTargets": 1,
    "successfulOperations": 2,
    "failedOperations": 1,
    "skippedOperations": 1
  },
  "targets": [
    {
      "repository": { "projectKey": "PROJ", "slug": "service-a" },
      "status": "success",
      "operations": [
        { "type": "repo.permission.user.grant", "status": "success" },
        { "type": "repo.settings.auto-merge", "status": "success" }
      ]
    },
    {
      "repository": { "projectKey": "PROJ", "slug": "service-b" },
      "status": "failed",
      "operations": [
        {
          "type": "repo.permission.user.grant",
          "status": "failed",
          "error": "user 'ci-deployer' not found",
          "errorKind": "not_found"
        },
        {
          "type": "repo.settings.auto-merge",
          "status": "skipped"
        }
      ]
    }
  ]
}
```

## Continuation and Remediation After Partial Apply

When `bb bulk apply` results in `partial_failure`, successful repositories are already updated. To safely complete rollout without re-executing unnecessary changes:

### Step 1: Query and Extract Failed Repositories

Use `bb bulk status` with `--json` and `jq` to list only the repositories and operations that failed:

```bash
bb bulk status <operation-id> --json | jq '.data.targets[] | select(.status == "failed") | {repo: "\(.repository.projectKey)/\(.repository.slug)", failed_operations: [.operations[] | select(.status == "failed")]}'
```

### Step 2: Remediate the Root Cause

Inspect the `errorKind` and `error` text:
- **`authorization`**: Ensure the running user or service token has project/repository admin permissions on the target repo.
- **`not_found`**: Check user, group, or project names in the policy for typos.
- **`validation`**: Adjust invalid field values or branch matcher references.

### Step 3: Create a Targeted Recovery Policy

Author a scoped recovery policy file (e.g. `recovery-policy.yaml`) whose `selector.repositories` contains only the failed repositories:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
selector:
  repositories:
    - PROJ/service-b
operations:
  - type: repo.permission.user.grant
    username: valid-ci-deployer
    permission: REPO_WRITE
  - type: repo.settings.auto-merge
    enabled: true
```

### Step 4: Plan, Review, and Apply Recovery Plan

Generate the deterministic recovery plan:
```bash
bb bulk plan -f recovery-policy.yaml -o .tmp/recovery-plan.json
```

Review `.tmp/recovery-plan.json` to confirm the targets and operations, then execute:
```bash
bb bulk apply --from-plan .tmp/recovery-plan.json
```

## Plan Storage, Auditing, and Retries

### 1. Storing Plan Artifacts for Compliance and Auditability

In enterprise governance, plan JSON artifacts can be committed to a pull request or archived in CI audit logs before execution. This ensures complete traceability of which changes were planned, who reviewed them, and what `planHash` was executed.

### 2. Reviewing Consecutive Plan Diffs

When policies evolve, compare the output of consecutive plan runs using standard diffing tools:

```bash
diff -u .tmp/plan-v1.json .tmp/plan-v2.json
```

### 3. Local Status Store Directory (`BB_BULK_STATUS_DIR`)

By default, operation status records are written to a `bulk-status` directory adjacent to your Bitbucket CLI configuration file.

In automated environments or ephemeral CI containers where you wish to preserve or redirect status records, set the `BB_BULK_STATUS_DIR` environment variable:

```bash
export BB_BULK_STATUS_DIR=/var/log/bb-bulk-status
bb bulk apply --from-plan .tmp/bulk-plan.json
```

## Output Modes and Scripting

All bulk commands support the global `--json` flag for machine consumption:

```bash
# Generate plan JSON to stdout
bb bulk plan -f policy.yaml --json

# Apply plan and output execution result envelope
bb bulk apply --from-plan .tmp/bulk-plan.json --json

# Query operation status envelope
bb bulk status <operation-id> --json
```

**A run that fails or is interrupted emits the error envelope, not the status envelope.**
Under `--json` a command writes exactly one document (ADR-075), and on failure that document
is the error. The status artifact is not lost — the error message names the operation id, and
`bb bulk status <id> --json` returns it:

```bash
bb bulk apply --from-plan .tmp/bulk-plan.json --json
# exit 5, stdout: {"version":"v2","error":{"kind":"conflict",
#                  "message":"bulk apply op-… completed with failures","exit_code":5}, …}

bb bulk status op-… --json    # the full artifact: what applied, what failed, what was skipped
```

So parse the id out of `.error.message` and fetch the artifact. Do not expect target detail
on the failure path.

### JSON Error Kinds

Responses follow the `bb.machine` v2 contract:

- `validation` (`exit 2`): Invalid policy syntax or unresolvable selectors.
- `authentication` (`exit 3`): Missing or invalid Bitbucket token.
- `authorization` (`exit 4`): Insufficient permissions (e.g. requires project or repo admin).
- `conflict` (`exit 5`): One or more targets failed during execution.
- `cancelled` (`exit 12`): Interrupted, or a deadline expired, before every repository was
  attempted. Not a retry signal — re-running replays mutations across the whole plan.
- `transient` / `internal`: Network connectivity issue or server failure.

## Error Reporting

If `bb bulk` behaves unexpectedly, check built-in command help:

```bash
bb bulk --help
bb bulk plan --help
bb bulk apply --help
bb bulk status --help
```

To file an issue with the maintainers:
```
https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/new
```
