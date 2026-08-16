# ADR 048: Documented commands are validated against the command tree

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `048`
- Title: `Documented commands are validated against the command tree`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/048-documented-commands-are-validated-against-the-command-tree.yaml`

## Decision

Every `bb ...` invocation inside a shell-tagged fenced block in the repository's Markdown is parsed against the real Cobra command tree by tools/docs-lint, which runs in quality:verify, in the pre-push hook and in CI. A documented command that does not resolve, uses a flag that does not exist, or passes the wrong number of positional arguments fails the build. Validation is static: the linter resolves the command, parses its flags and applies its argument rules without executing anything and without contacting a Bitbucket instance. Blocks that deliberately show a malformed invocation are marked with an HTML comment, `<!-- docs-lint: expect-invalid -->`, which inverts the check rather than skipping it. Such a block fails if its command becomes valid. Blocks tagged text, json or yaml are out of scope: they hold output rather than commands. That is what excludes the generated command reference, whose `bb ...` lines are Cobra usage strings and are correct by construction.

## Agent Instructions

Run `task docs:lint` after changing documentation or the command tree. It is part of `task quality:verify`, so the pre-push hook covers it. Write examples in ```bash blocks so they are checked. A snippet placed in an unmarked or text block is invisible to the linter, which is a way of avoiding the check rather than passing it. When an example is meant to be invalid, mark the block with `<!-- docs-lint: expect-invalid -->` rather than reformatting it to escape the check. Do not add an unconditional ignore directive. When the linter reports something you believe is correct documentation, suspect the linter and check for a trailing carriage return first: a CRLF checkout puts \r inside the last token, and pflag reports that as an unknown flag with no visible difference in the message.

## Rationale

Six documented invocations did not parse at v2.0.2, including the README quickstart — the first command a new user copy-pastes — and an example in skills/bb/SKILL.md, which agents emit verbatim. They were found by hand during an external review, which is not a process that repeats. Documentation that does not run is worse than missing documentation. A missing example sends the reader to --help; a broken one sends them debugging their own environment for a mistake that is in the repository. For the agent consumer it is worse still: the skill is the entry point, and an agent that emits a documented command gets an error it cannot attribute. The check is cheap because the command tree is already in-process. It needs no Bitbucket instance, so it runs in the fast CI job, in the pre-push hook and on a fresh clone. The inverted directive rather than a plain ignore is deliberate. An unconditional exemption is invisible once added and rots silently; a block that asserts its example is invalid keeps reporting when that stops being true.

## Rejected Alternatives

- `Fix the six invocations and rely on review to catch the next ones`: They accumulated under review already. The invocations are in the highest-traffic pages, and a reviewer reading prose does not mentally parse each flag against a 224-command tree.
- `Execute the documented commands against the live test instance`: Would catch semantic errors as well as syntactic ones, but requires a running Bitbucket for what is otherwise a static check, cannot run on a fresh clone or in the fast CI job, and would need every example to be safe to execute — including the mutating ones.
- `Generate all examples from the command tree, as the command reference already is`: Correct by construction, but the value of hand-written examples is precisely that they show realistic combinations and ordering that a generator does not know about. Validating hand-written examples keeps that value and removes the failure mode.
- `Add a plain ignore directive for awkward cases`: Every exemption mechanism is eventually used to silence a real failure. An assertion that the example is invalid covers the only case actually encountered and cannot be used to hide a command that broke.
