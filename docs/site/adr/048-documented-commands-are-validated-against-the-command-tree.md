---
search:
  boost: 0.3
---

# ADR-048: Documented commands are validated against the command tree

tools/docs-lint parses every `bb` invocation the documentation shows against the real Cobra command tree: it resolves the command, parses its flags and applies its argument rules, without running anything or contacting Bitbucket. It reads README.md, AGENTS.md, CONTRIBUTING.md, SECURITY.md, docs/ and skills/. A command that does not resolve, a flag that does not exist and the wrong number of arguments all fail.

What is checked:

- Every `bb` line in a shell block (`bash`, `sh`, `shell`, `console`, `zsh`). A block marked `<!-- docs-lint: expect-invalid -->` shows a malformed invocation on purpose, and fails if its command becomes valid.
- Every inline code span that begins with `bb`, outside the decision records and the release notes, which name commands as they were. A placeholder after a flag leaves the flag checked and its value not; a placeholder where a subcommand would stand makes the span a shape rather than an invocation. A passage that names commands which do not exist on purpose is marked `<!-- docs-lint: ignore-inline -->`, which exempts it up to the next blank line.
- In a `json` or `yaml` block, an invocation a client launches from configuration, `"command": "bb"` beside an `"args"` array, by the rules a shell line gets.
- In a `json` or `yaml` block, machine output, against the schema its command declares, read from the lookup `--describe` answers from. The block says which command it shows with `<!-- docs-lint: output-of bb <command> -->`, or that it illustrates the envelope with `<!-- docs-lint: envelope-shape -->`, which is still held to the envelope rules. A machine-output block with neither fails, so a new example is checked without anyone remembering to mark it.

Text blocks are out of scope. The generated command reference sets its usage lines in them, since they are correct by construction, and its hand-written examples in shell blocks, so they are checked.

Run `task docs:lint` after changing documentation or the command tree; it is part of `task quality:verify`, so the pre-push hook and CI run it too. Write examples in `bash` blocks: a snippet in an untagged or text block is invisible to the linter, which avoids the check rather than passing it. Mark an example meant to be invalid with expect-invalid rather than reformatting it, and mark an inline passage with ignore-inline only when it names commands that do not exist on purpose, such as a page listing what bb deliberately lacks, with the reason in the directive. It is the one plain exemption, because such a passage can never pass. Do not reach for envelope-shape to quiet a schema failure: the example and the command disagree, and the command is right. Read a payload from the command's `--describe` output rather than writing it from memory.

Documentation that does not run is worse than none. A missing example sends the reader to `--help`; a broken one sends them debugging their own setup for a mistake in the repository, and an agent that emits a documented command verbatim gets an error it cannot attribute. The command tree and the schema lookup are in-process, so the check needs no Bitbucket and runs on a fresh clone and in the fast CI job. A directive that asserts a block is invalid keeps reporting when that stops being true, where a plain exemption is invisible once added and rots.

## Not chosen

- **Rely on review to catch broken invocations**: A reviewer reading prose does not parse each flag against the command tree, and the broken ones sit in the most-read pages.
- **Execute the documented commands against the live test instance**: Would catch semantic errors as well as syntactic ones, but requires a running Bitbucket for what is otherwise a static check, cannot run on a fresh clone or in the fast CI job, and would need every example to be safe to execute — including the mutating ones.
- **Generate all examples from the command tree, as the command reference already is**: Correct by construction, but the value of hand-written examples is precisely that they show realistic combinations and ordering that a generator does not know about. Validating hand-written examples keeps that value and removes the failure mode.
- **Add a plain ignore directive for awkward blocks**: Every exemption mechanism is eventually used to silence a real failure. An assertion that the example is invalid covers the case actually met and cannot hide a command that broke.
- **Infer which command an output example belongs to from the nearest preceding invocation**: A shell block often lists several commands, and the output shown belongs to one of them rather than to the last. A declared binding also tells a human reader which command produced the block.
