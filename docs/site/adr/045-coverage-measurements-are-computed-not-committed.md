---
search:
  boost: 0.3
---

# ADR-045: Compute coverage measurements, commit only verifiable baselines

A quality artifact is either a measurement or a baseline. A measurement is the output of running the suite: the coverage profiles, the combined report, which releases passed the live suite. It is recomputed on every run into .tmp/, published by CI to Codecov and as workflow artifacts, and never committed. A baseline asserts what must remain true, such as which commands the live suite reaches. It is committed under docs/quality/, small and readable in a diff, and it has a verify task that fails when the committed copy is stale and needs no Bitbucket instance, so it gates every pull request. docs/quality/README.md lists each baseline with the tasks that update and verify it.

Do not commit coverage profiles or reports; coverage.out, docs/quality/coverage-report.json and docs/quality/coverage.combined.*.out are gitignored. Regenerate a baseline in the change that affects it. When you add a file under docs/quality/, add its verify task in the same change; if it cannot be verified without a Bitbucket instance, it does not belong there. A rebase needs nothing regenerated: `task pr:rebase`, or `git rebase origin/next`. When patch coverage fails, do not run the suite again to find out where. The gate prints the uncovered changed lines, and `task quality:coverage:replay` re-applies every threshold to the profiles already in .tmp/ in seconds. Close the gap with tests.

A measurement changes on nearly every branch, so a committed one conflicts on every rebase, and nothing would read it: CI computes coverage afresh. A baseline changes only when what it asserts changes, and that change is what a reviewer needs to see; a diff in command-reach.json is how a command losing live coverage shows. A baseline with no verification is not a baseline, but a file that goes quietly wrong.

## Not chosen

- **Commit the coverage report and verify it in CI**: It would still change on nearly every branch and conflict on every rebase, and verifying it turns each conflict into a failing build. Codecov already keeps the history.
- **Stop committing the baselines too**: They are contracts rather than measurements, verifiable without a Bitbucket instance, and a change in one is what a reviewer should see.
- **Report only a percentage when patch coverage fails**: It says the patch is short without saying where, while the profile that would say sits on a runner. The tool knows the uncovered lines when it computes the number, so printing them costs nothing.
