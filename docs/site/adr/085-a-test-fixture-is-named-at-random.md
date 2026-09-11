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

A test that creates a fixture on a server names it with a random identifier, from `testsupport.UniqueSuffix`. Not a timestamp, not a timestamp with a counter beside it, and not a timestamp cut down to fit.
The prefix stays readable, because a fixture left behind on the instance has to be traceable to the test that made it. Only the unique part is random.

## Agent Instructions

Use `testsupport.UniqueSuffix` or `testsupport.UniqueName` for anything a test creates that the server requires to be unique: projects, repositories, branches, tags, labels, tokens, webhooks, build and report keys. Do not name a fixture from `time.Now()` in any form, and do not make a clock unique by appending a counter to it. A governance test fails the build on either. A test that asserts on time itself is not a fixture name and is not covered by this.

## Rationale

Clock-derived names collided here in three distinct ways, each found only after the failure had been misread as a product bug. Truncated: 65 names used `UnixNano()%100000`, which repeats every 100 microseconds while the suite runs eight ways in parallel. Bitbucket answered the duplicate label with `500 A database error has occurred`, which reads as a server defect. Coarse: `time.Now()` is not guaranteed finer than a millisecond and on Windows often is not, so two goroutines can read the same value however many digits it carries. Restarted: a process-local counter beside a low-resolution clock starts again from the beginning on the next run, so a run that crashed and left fixtures behind collides with the run after it. Randomness removes all three at once, and removes the reasoning: there is no window to argue about, no resolution to depend on, and no state that resets.

## Rejected Alternatives

- `Keep timestamps and widen them until collisions stop`: Each widening answers one of the three causes and leaves the others. A full-resolution timestamp still repeats when the clock is coarse, and still repeats across runs when it is combined with a counter.
- `Clean the instance before every run`: It makes the suite depend on a teardown that a crash is precisely what prevents, and does nothing about two tests colliding inside one run.
