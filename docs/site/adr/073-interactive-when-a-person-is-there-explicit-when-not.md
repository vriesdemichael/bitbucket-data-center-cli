---
search:
  boost: 0.3
---

# ADR-073: Interactive when a person is there, explicit when not

bb may prompt, and never guesses whether it should: `interactive.Detect` decides (ADR-072). Refusing to prompt is not permission to proceed. When nobody can be asked and a required value is missing, the command fails with a message naming every flag that would have supplied one. It does not fall back to a default, and it does not run with the value blank. A value read from where the command runs is not a default, and may stand in for its flag as ADR-102 says. Input that is missing, invalid or ambiguous fails before anything is changed, and before any request where bb can tell without one, as a validation error naming the flag or the argument the command takes, and for a flag with a fixed set of values, the values. Every command stays completely drivable by flags, pipes and environment variables.

A command named delete, remove, revoke or clear is destructive. A person is asked to type back what it is about to destroy; where there is no one thing to name, as when clearing every GPG key, the question is yes or no. With nobody to ask, the command requires `--yes`. Machine output is nobody to ask, so under `--json` or `--yaml` it refuses without `--yes` like any other caller. `--yes` counts only when the target is named: on a repository taken from the git remote it is refused. A dry run asks for no confirmation. No command reads standard input unless the caller asked it to: with `-` as an argument or a flag's value, with a stdin flag such as `--token-stdin`, or by running a command whose protocol is stdin, the MCP server and the git credential helper. An implicit fallback to stdin is prohibited even where nothing is printed, because stdin held open with no data is indistinguishable from stdin about to deliver data.

Prompt through internal/cli/prompt, which asks `interactive.Detect`: `FillMissing` for missing values, `ConfirmDeleteOf` or `ConfirmDestructive` for a destructive command, and `ConfirmAction` where there is no one target to name. Do not add a prompt package or hand-roll a scan. A destructive command gets `--yes` and its question from the walk over the tree by its name; `TestEveryDestructiveCommandCanBeConfirmed` fails on one without `--yes`. Give it an explicit target. Do not read stdin without an explicit `-` or a stdin flag; `TestEveryUseOfStandardInputIsAccountedFor` fails on a new file that names it.

A ban on prompting guards the wrong half. Prompts are visible and get reviewed; an unguarded read of stdin is neither, and it is what actually hangs. What makes prompting safe is not detection, which ADR-072 shows cannot be trusted on its own, but a complete non-interactive path: it refuses, says which flag is missing, and exits. An agent that meets that message can fix its own call, which is worth more than never being asked. `--repo` is filled in from the git remote, so a `--yes` that applied to it would delete the repository underfoot.

## Not chosen

- **Never prompt**: Removes the feature for the person it serves, and says nothing about reading stdin, which is what hangs.
- **Prompt whenever a terminal is attached, without an escape hatch**: A terminal is not evidence that anyone will answer; ADR-072 has the reasons.
- **Let a refused prompt fall back to a safe default**: Silently proceeds with a value nobody chose. Refusing to ask is not permission to guess.
- **Treat machine output as a confirmation**: A format flag says nothing about intent, and should not be what authorises a deletion.
