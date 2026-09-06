# Bulk Operations

## Why bulk workflows exist

Bulk workflows apply consistent policy changes across many repositories while keeping reviewable,
deterministic plan artifacts.

## Workflow model

1. Author policy file (YAML or JSON).
2. Run `bb bulk plan` to produce a reviewed plan with a deterministic `planHash`.
3. Review and store plan artifact.
4. Run `bb bulk apply --from-plan ...` to execute exactly that reviewed plan.
5. Query operation status with `bb bulk status <operation-id>`.

## Example policy

See: `docs/examples/bulk-policy.yaml`.

```bash
bb bulk plan -f docs/examples/bulk-policy.yaml -o .tmp/bulk-plan.json
bb bulk apply --from-plan .tmp/bulk-plan.json
bb bulk status <operation-id>
```

## Safety and contract notes

- `bulk plan` performs no server writes.
- `bulk apply` does not support `--dry-run`; use `bulk plan` as preview.
- `bulk apply` persists status artifacts under local config directory (override with `BB_BULK_STATUS_DIR`).
- JSON mode is available for plan/apply/status commands using global `--json`.

## Secrets in a policy

A policy never holds a credential. `repo.webhook.create` takes `secretEnv` and
`credentialsPasswordEnv`, which name an environment variable that `bb bulk apply`
reads at apply time — so the plan artifact stays committable, and the same plan
applies against different environments unedited. An apply whose named variable is
unset is refused rather than run without the secret.

See [Webhook Secrets](webhook-secrets.md) for the full rules.

## Schema and IDE integration

- Schemas are published under [JSON Schemas](../reference/schemas.md).
- Add YAML schema comment for editor validation and completion:

```yaml
# yaml-language-server: $schema=https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/reference/schemas/bulk-policy.schema.json
```
