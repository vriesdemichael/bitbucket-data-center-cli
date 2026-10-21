# Change Many Repositories

Bitbucket keeps some settings on the project and applies them to every
repository in it: permissions, webhooks, default tasks and branch restrictions.
Set those once, with [`bb project`](../reference/commands/project.md), and there
is nothing to repeat.

For a change Bitbucket keeps per repository, or one for only some of a project's
repositories, run the command once per repository: list the repositories,
preview the change in each, then make it.

## List the repositories

`bb repo list --json` lists each repository with its `slug`. Pick the ones the
change is for, here those whose slug starts with `svc-`:

=== "Bash"

    ```bash
    bb repo list --project PAY --all --json |
      jq -r '.data[].slug | select(startswith("svc-"))'
    ```

=== "PowerShell"

    ```powershell
    (bb repo list --project PAY --all --json | ConvertFrom-Json).data.slug |
      Where-Object { $_ -like 'svc-*' }
    ```

`--all` lists every repository, rather than the first 25.

## Preview, then change

Run the change with `--dry-run` in every repository before making it in any. A
dry run exits as the real run would, so the script stops at the first
repository that would refuse the change, before anything has changed:

=== "Bash"

    ```bash
    #!/usr/bin/env bash
    set -euo pipefail

    project=PAY
    slugs=$(bb repo list --project "$project" --all --json |
      jq -r '.data[].slug | select(startswith("svc-"))')

    # Preview every repository first. A dry run exits as the real run would, so
    # set -e stops the script at the first repository that would be refused.
    for slug in $slugs; do
      bb webhook create ci https://ci.example.com/hooks/bitbucket \
        --repo "$project/$slug" --dry-run
    done

    for slug in $slugs; do
      bb webhook create ci https://ci.example.com/hooks/bitbucket \
        --repo "$project/$slug"
    done
    ```

=== "PowerShell"

    ```powershell
    $ErrorActionPreference = 'Stop'

    $project = 'PAY'
    $slugs = (bb repo list --project $project --all --json | ConvertFrom-Json).data.slug |
      Where-Object { $_ -like 'svc-*' }

    # Preview every repository first, and stop at the first that would be refused.
    foreach ($slug in $slugs) {
      bb webhook create ci https://ci.example.com/hooks/bitbucket --repo "$project/$slug" --dry-run
      if ($LASTEXITCODE -ne 0) { throw "bb would refuse the change in $project/$slug" }
    }

    foreach ($slug in $slugs) {
      bb webhook create ci https://ci.example.com/hooks/bitbucket --repo "$project/$slug"
      if ($LASTEXITCODE -ne 0) { throw "bb did not make the change in $project/$slug" }
    }
    ```

Each preview names the repository and what would happen there:

```text
Dry run: bb webhook create would go through (preconditions-checked)
- create PAY/svc-orders active=true events=[repo:refs_changed] name=ci url=https://ci.example.com/hooks/bitbucket: would apply
    webhook will be created
```

What each command's dry run checks is under "Dry run" in its `--help`, and
[Dry-Run Planning](../advanced/dry-run-planning.md) says how far to trust it.

## Run it again

A script like this can be run again, after a failure or for a repository added
since. `bb webhook create` finds a webhook with the same name, URL and settings
and creates nothing, and its dry run says so:

```text
Dry run: bb webhook create would change nothing (preconditions-checked)
- create PAY/svc-billing active=true events=[repo:refs_changed] name=ci url=https://ci.example.com/hooks/bitbucket: no change
    webhook 33 already exists with this name, URL and settings, so nothing will be created
```

Before you loop another command, check what it does the second time: its dry
run in a repository where the change is already made says `no change`, or that
it would make the change again.

## Read what changed

Under `--json` each run is one document, which says what it did. For
`bb webhook create`, `created` is false where the webhook was already there:

```bash
bb webhook create ci https://ci.example.com/hooks/bitbucket --repo PAY/svc-orders --json |
  jq '.data.created'
```

`bb <command> --describe` lists the fields a command's document has.
