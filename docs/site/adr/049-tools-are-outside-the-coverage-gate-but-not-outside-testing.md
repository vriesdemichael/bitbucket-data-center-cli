---
search:
  boost: 0.3
---

# ADR-049: Build tooling stays outside the coverage gate but not outside testing

The coverage gates measure cmd/ and internal/ without generated code (ADR-065); tools/ is outside them. Tools are tested anyway, by convention rather than by a threshold. Logic under tools/ that can be exercised in-process, such as parsers, tokenisers, path resolution, and diff and coverage arithmetic, gets table-driven unit tests in the change that adds or substantially changes it, and a reviewer asks when they are missing. main(), flag registration and os.Exit plumbing are exempt. A tool that computes something the project relies on is held to the standard of the gate it feeds: tools/quality-report produces the numbers every coverage gate reads, so a bug in it makes the build pass when it should not.

Do not add tools/ to -scope-include, and do not lower a threshold to accommodate a change to tools/. The absence of a gate is not permission to skip tests; it is why the convention is written down. Do not write a test whose only purpose is to move a percentage. Judge what to test by whether a bug would be caught elsewhere: a missing flag or a bad path fails loudly on the next run, while tokenising, path resolution and arithmetic fail quietly and wrongly. Test the second kind.

The global floor runs with little headroom, and bringing tools/ into scope would cut it far enough that an unrelated edit to a tool could fail the build; the usual remedy, lowering the threshold, would weaken the gate for the shipped CLI, where it matters most. A line gate also cannot tell main() plumbing from parsing logic, so it would demand tests where a test proves nothing. Measure again before reopening the question; the figures move with the tree, which is why they are not written here. The convention relies on review rather than automation, which is weaker, and that is accepted.

## Not chosen

- **Add tools/ to the coverage scope**: Leaves the global floor too little headroom to survive changes that have nothing to do with it, or needs the threshold lowered for everything.
- **Include tools/ in patch coverage only**: Targets the real concern, new tool code arriving untested, but still cannot tell logic from main() plumbing, so it demands the same ceremony. Worth revisiting if the convention proves insufficient.
- **Require every tools/ package to have a _test.go**: Satisfied by one meaningless test, and a check that can be satisfied without doing the work tends to be. It remains available as a complement if the convention erodes.
- **Leave tooling untested and rely on the build breaking**: Adequate for tools whose failures are loud, and wrong for the ones that compute. A defect in quality-report does not break the build; it makes it pass.
