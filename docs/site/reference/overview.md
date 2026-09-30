# Command Reference Overview

## Generated reference model

The command reference is generated from the CLI command tree: an index of every command, and a
page per top-level command. Each command's entry carries what `bb ... --help` prints, and the
document it writes under `--json`:

- Description and usage
- Examples
- Flags, including those inherited from a command group
- What `--dry-run` checks
- Output fields, as `bb ... --describe` outlines them

The flags every command takes are on a page of their own, [Global flags](commands/global-flags.md).

Source and generation path:

- Command tree source: `internal/cli/`
- Export tool: `tools/cli-docs-export/`
- Generated pages: `docs/site/reference/commands/`

## Regenerate command docs

```bash
task docs:export-command-reference
```

or regenerate all docs artifacts:

```bash
task docs:generate
```

## Drift checks

The docs workflow verifies generated content is up to date:

```bash
task docs:verify-generated
```

Use [All Commands](commands/index.md) for full command and argument details.
