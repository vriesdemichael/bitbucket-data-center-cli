---
search:
  boost: 0.3
---

# ADR 085: A test fixture is named at random, not from the clock

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `085`
- Title: `A test fixture is named at random, not from the clock`
- Category: `development`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/085-a-test-fixture-is-named-at-random.yaml`

## Decision

A test that creates a fixture on a server names it with testsupport.UniqueSuffix or testsupport.UniqueName, which draw from crypto/rand. Not a timestamp, not a timestamp with a counter beside it, and not a timestamp cut down to fit. The prefix stays readable, so a fixture left behind can be traced to the test that made it; only the unique part is random. This refines ADR-015's unique per-test namespaces.

## Agent Instructions

Use testsupport.UniqueSuffix or testsupport.UniqueName for anything a test creates that the server requires to be unique. Upper-case a value Bitbucket stores upper-cased, such as a project key. TestNoFixtureIsNamedFromTheClock fails a string built from time.Now() through fmt.Sprint*, strconv or concatenation, directly or through a local variable. A clock value that is not a name, such as a query window, carries a clock-value-not-a-name comment giving the reason.

## Rationale

Clock-derived names collided in three ways, each first misread as a product bug. Truncated, they repeat within a run. The clock is coarser than the suite is parallel, so two tests read the same value. And a counter beside the clock restarts with the process, so a run collides with the fixtures a crashed run left behind. Randomness removes all three, and the reasoning about them.

## Rejected Alternatives

- `Widen the timestamps until collisions stop`: Each widening answers one cause and leaves the others: a full-resolution timestamp still repeats when the clock is coarse, and across runs when a counter is what makes it unique.
- `Clean the instance before every run`: It depends on a teardown that a crash is exactly what prevents, and does nothing about two tests colliding inside one run.
