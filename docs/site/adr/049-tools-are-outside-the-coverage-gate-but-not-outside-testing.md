# ADR 049: Build tooling stays outside the coverage gate but not outside testing

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `049`
- Title: `Build tooling stays outside the coverage gate but not outside testing`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/049-tools-are-outside-the-coverage-gate-but-not-outside-testing.yaml`

## Decision

Keep the coverage scope at cmd/ + internal/, excluding generated paths. tools/ is deliberately not measured by the global or patch coverage gates defined in ADR-005. Test tools/ anyway, as a convention rather than a threshold. Anything in tools/ with real logic that can be exercised in-process — parsers, tokenisers, resolvers, diff and coverage arithmetic, anything with branches worth getting wrong — gets unit tests in the same change that introduces it. A reviewer should expect them and ask when they are missing. Do not write tests whose only purpose is to move a percentage. main(), flag registration and os.Exit plumbing are exempt: they are the parts a gate would have forced and the parts where a test proves nothing.

## Agent Instructions

Do not add tools/ to -scope-include, and do not lower a coverage threshold to accommodate a change to tools/. The gate is scoped to the shipped CLI on purpose. When you write or substantially change a tool under tools/, add table-driven unit tests for its logic in the same change. The absence of a gate is not permission to skip them; it is the reason the convention has to be written down. Judge what to test by whether a bug would be caught elsewhere. Tokenising, path resolution and arithmetic fail quietly and wrongly; a missing flag or a bad file path fails loudly on the next run. Test the first kind. Where a tool computes something the project relies on — quality-report most of all, since it produces the numbers every other gate reads — treat a bug as equivalent to a bug in the gate itself, and test accordingly.

## Rationale

The question was measured rather than argued. Adding tools/ to the scope moves global combined coverage far enough to leave barely a point of margin over the floor, where there had been several. At that margin an unrelated edit to a tool can fail the build for reasons disconnected from the change, and the usual remedy — lowering the threshold — would weaken the gate for cmd/ and internal/, where it matters most. Rerun the measurement before reopening the question; the figures move with the tree, which is why they are not written down here. The composition of what the gate would have demanded is the other half. On the change that raised the question, 65 new lines in tools/ were uncovered: roughly a third main() and os.Exit plumbing, a third I/O error paths needing filesystem fault injection, and a third genuine parsing logic. A gate cannot tell those apart, so it would have bought about a third real tests and about two-thirds ceremony — the same trade ADR-045 rejected when it deleted the committed coverage artifacts. The counter-argument is real and is why the convention exists rather than nothing. Tool failures are not all loud. A bug in quality-report makes gates pass when they should not, which is silent and worse than a broken build; command-reach exists precisely because the project already concluded that a check you cannot trust is worse than no check. quality-report is also the largest tool and the least covered, which is uncomfortable regardless of where the scope line sits. Writing the convention down is what keeps that concern addressed without paying for the gate. It relies on review rather than automation, which is weaker; that is accepted deliberately, with the measurements recorded here so the trade can be re-examined rather than re-derived.

## Rejected Alternatives

- `Add tools/ to the coverage scope`: Cuts the margin over the floor to about a point, making the gate fragile for changes that have nothing to do with it, and would require either a retro-testing campaign across all of tools/ first — including two packages with no test files at all — or lowering the threshold and weakening it everywhere else.
- `Include tools/ in patch coverage only, leaving the global gate alone`: Genuinely narrower, and it targets the actual concern: new tool code arriving untested. It needs quality-report to take separate include lists for the two gates, which is a small change. Rejected for now because it still cannot distinguish real logic from main() plumbing, so it would demand the same ceremony on the same third of the lines. Worth revisiting if the convention proves insufficient.
- `Require every tools/ package to have at least one _test.go`: Cheap, automatable, and catches the two packages with no tests. Rejected as a substitute because it is trivially satisfied by one meaningless test, and a check that can be satisfied without doing the work tends to be. It remains available as a complement if the convention erodes.
- `Leave tooling untested and rely on the build breaking`: Adequate for the tools whose failures are loud, and wrong for the ones that compute rather than execute. A quality-report defect does not break the build; it makes the build pass.
