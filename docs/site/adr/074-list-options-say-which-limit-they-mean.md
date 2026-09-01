# ADR 074: List options say which limit they mean

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `074`
- Title: `List options say which limit they mean`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/074-list-options-say-which-limit-they-mean.yaml`

## Decision

A service list option names its pagination field for what it does. MaxResults caps the total returned and truncates; PageSize is the per-request page size and the call pages to exhaustion. Neither is called Limit. A caller that reads MaxResults: 100 and gets at most 100 is not surprised, and one that reads PageSize: 100 does not expect a cap. TestNoServiceOptionIsCalledLimit fails on a new one. Zero means the service's default page size, not unlimited. No service can currently be asked for everything; a caller that needs it must say how many it will accept.

## Agent Instructions

Name a new list option MaxResults or PageSize. Do not add a field called Limit, and do not reuse a neighbouring service's name without checking which behaviour it has. Do not pass zero to mean unlimited. It yields one default-sized page, which is the smaller answer and the one that fails silently. A CLI --limit flag is a total, so it maps to MaxResults.

## Rationale

Both semantics are legitimate: a display command wants a total, a lookup wants to page to exhaustion. The defect was that the signatures were identical, so a call site could not tell which it was getting, and the wrong guess failed silently and in the unsafe direction -- fewer results, no error, no flag. Eleven services capped and eight paged. One of the nineteen documented which, and its comment said "unlike other service list options", which was true when written and by then applied to a third of them. Two user-visible bugs came from reading the parameter as a page size, which is the natural reading and one nothing at the call site contradicted. Renaming rather than documenting is the point: a comment does not stop the next call site, and the compiler re-examined every existing one exactly once.

## Rejected Alternatives

- `Document the two meanings consistently instead`: Documentation is what was already missing, and it does not fail a build.
- `Make every list exhaustive and cap at the CLI layer`: Better shape, much larger change, and it would move truncation away from the service that knows the page boundaries. Worth revisiting if a service ever needs both.
- `Return a truncated flag alongside the results`: Composes with the envelope's meta.limitReached and is worth having, but it makes truncation observable rather than unambiguous. Naming does the second, which is what was wrong.
