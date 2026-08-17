<!--
Three prompts, no checklist.

CI already runs the formatting, coverage, docs, ADR and live gates, and it knows
whether they passed better than a ticked box does. CONTRIBUTING.md documents what
those gates are. The only things asked for here are the ones no gate can work out
on its own.

Delete any section that genuinely does not apply.
-->

## What

<!-- One or two sentences. What does this change do? -->

## Why

<!--
The reasoning, not a restatement of the diff. What made the current behaviour
wrong, or what does this make possible that was not possible before? A sentence
is often enough; the bar is that a reader six months from now can tell whether
the premise still holds.
-->

## Decision record

<!--
Does this change a contract, a default, a flag's meaning, or the shape of output
somebody parses? If so it is a decision, not just a change, and it probably wants
an ADR in docs/decisions/ — see the existing ones for the shape, and
`task quality:validate-decisions` to check it.

If it does not, say "no ADR needed" and delete the rest.
-->

## Related issues

<!-- Closes #123 / Part of #123 / none -->
