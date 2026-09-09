---
search:
  boost: 1.5
---

# Bulk Operations

`bb bulk` applies one reviewed change across many repositories. It is a
three-step workflow — write a policy, plan it, apply the plan — and the split
exists so that what gets applied is a thing you looked at rather than a
selector evaluated at the moment of writing.

```bash
bb bulk plan --file policy.yaml --output plan.json
# read plan.json
bb bulk apply --from-plan plan.json
```

## The three commands

| Command | Does |
|---|---|
| `bb bulk plan --file <policy>` | Resolves the policy into a deterministic plan. Contacts the server to enumerate repositories; changes nothing |
| `bb bulk apply --from-plan <plan>` | Applies the operations in a reviewed plan |
| `bb bulk status <operation-id>` | Shows the saved status of a prior apply |

`plan` takes `--output` to write the plan artifact; without it the plan is
printed. `apply` reads only a plan file — it never takes a policy, so the thing
applied is always the thing reviewed.

`bb bulk apply` rejects `--dry-run`. The plan *is* the dry run, and accepting the
flag would imply a second, weaker preview exists.

## Choosing repositories

The `selector` decides the blast radius. It has three forms, and they are
additive — the targets are the union of whatever you use.

```yaml
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PLATFORM
  repoPattern: "*-service"
  repositories:
    - PAYMENTS/ledger
    - legacy-gateway
```

**`projectKey` alone** selects every repository in that project.

**`repoPattern`** filters the slugs within `projectKey`, so it requires one. It
is a **glob, not a regular expression** — `path.Match` semantics:

| Pattern | Matches |
|---|---|
| `*-service` | `payments-service`, `auth-service` |
| `payments-*` | `payments-api`, `payments-worker` |
| `svc-?` | `svc-a`, `svc-b` |
| `[a-m]*` | slugs starting `a` through `m` |

A pattern must match the **whole** slug: `service` matches a repository called
exactly `service`, not `payments-service`. Regular-expression habits do not
carry over — `.*-service` looks for a literal dot and matches nothing. An
invalid pattern is refused at plan time, naming `selector.repoPattern`.

**`repositories`** names repositories explicitly, and is the only form that
crosses projects. An entry may be `PROJECT/slug`, which wins over
`selector.projectKey`, or a bare slug, which uses it. A bare slug with no
`projectKey` set is refused.

So a pattern is confined to one project, and a policy that has to reach across
several lists them.

A selector matching nothing is an error rather than a successful run over
nothing.

## Operations

Each entry in `operations` names a `type` and carries that type's fields.

| Type | Requires |
|---|---|
| `repo.permission.user.grant` | `username`, `permission` |
| `repo.permission.group.grant` | `group`, `permission` |
| `repo.webhook.create` | `name`, `url` |
| `repo.settings.auto-merge` | `enabled` |
| `repo.settings.auto-decline` | `enabled` |
| `repo.pull-request-settings.required-approvers-count` | `count` |
| `repo.pull-request-settings.required-all-tasks-complete` | `requiredAllTasksComplete` |
| `repo.default-task.create` | `description` |
| `build.required.create` | `payload` |

A policy naming an unknown type, or omitting a required field, is refused at
plan time with exit `2` and no server contact.

```yaml
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PLATFORM
  repoPattern: "*-service"
operations:
  - type: repo.permission.group.grant
    group: platform-reviewers
    permission: REPO_WRITE
  - type: repo.settings.auto-merge
    enabled: true
```

## Secrets

A policy is a file that gets committed and reviewed, so it names the environment
variable holding a secret rather than the secret. See
[Webhook Secrets](webhook-secrets.md).

## Plan integrity

A plan is a reviewed artifact, and `apply` treats it as one. A plan edited after
review — an added target, a fabricated operation — is rejected rather than
applied, so review means something.

## Schema and editor support

The policy schema is published, so an editor can validate as you type:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
apiVersion: bb.io/v1alpha1
```

The plan and status artifacts have published schemas too, under
[JSON Schemas](../reference/schemas.md).

## Where artifacts live

`bb bulk` writes plan and run state to the OS temp directory by default. Set
`BB_BULK_STATUS_DIR` somewhere durable if runs need to survive a reboot, or
somewhere shared for a team runner — see
[Environment Variables](../reference/environment.md#bulk-operations).

## See also

- [Dry-Run Planning](dry-run-planning.md) — the single-command equivalent
- [Webhook Secrets](webhook-secrets.md)
- [JSON Schemas](../reference/schemas.md)
