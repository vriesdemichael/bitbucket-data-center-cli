# Bulk Operations

!!! danger "Deprecated — due for removal in v5.0.0"

    `bb bulk` works and is supported until it is removed, and warns on stderr on
    every invocation. **Do not start with it.** If you already use it, the
    migration is below.

    **Why.** Its nine operations are all repository *configuration*, and four of
    them — user and group permissions, webhooks, default tasks — are settable
    once at the **project** level, where Bitbucket cascades them to every
    repository it contains. Doing those per repository is more work for the same
    result. What remains is a handful of repository-only settings, and a shell
    loop over `bb` reaches those with more flexibility than a policy file and
    without a plan artifact to manage.

    It also never grew into what a bulk tool is actually wanted for: changing
    *code* across an estate — branch, run a script, commit, open a pull request
    with reviewers. That needs a different shape, and this one was not going to
    become it.

    **What to use instead.**

    | Instead of | Use |
    |---|---|
    | `repo.permission.user.grant`, `repo.permission.group.grant` | `bb project permissions` |
    | `repo.webhook.create` | `bb project webhook` |
    | `repo.default-task.create` | `bb project default-task` |
    | the repository-only settings | `bb repo settings ...` in a loop over `bb repo list` |
    | cross-repository code changes | a tool built for it, such as multi-gitter or Sourcegraph batch changes |

    `bb project branch-restriction` is worth knowing about too: it has no
    repository-level equivalent, so it was never reachable through bulk at all.

The rest of this page is reference for a policy that already exists: the three
commands, what a selector reaches, and the fields each operation takes. It is
not a place to start — see the migration table above for that.

`bb bulk` applies one reviewed change across many repositories. It is a
three-step workflow — write a policy, plan it, apply the plan — and the split
exists so that what gets applied is a thing you looked at rather than a
selector evaluated at the moment of writing.

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
| `repo.settings.auto-decline` | `enabled`; `inactivityWeeks` when enabled |
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

## What a plan artifact holds

`--output` writes the reviewed plan as JSON. It carries the policy it came from,
the targets it resolved to, and the hash that ties an apply back to it:

```text
apiVersion  bb.io/v1alpha1
kind        BulkPlan
planHash    sha256:b249cf...
policy      the policy as given, normalised
validation  valid, plus any errors
summary     targetCount, operationCount
targets     each repository, with the operations queued for it
```

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

`bb bulk` writes run state to `bulk-status` beside the stored configuration
file, so it follows `BB_CONFIG_PATH`; a plan is written only where `--output`
says. Set `BB_BULK_STATUS_DIR` somewhere shared for a team runner — see
[Environment Variables](../reference/environment.md#bulk-operations).

## See also

- [Dry-Run Planning](dry-run-planning.md) — the single-command equivalent
- [Webhook Secrets](webhook-secrets.md)
- [JSON Schemas](../reference/schemas.md)
