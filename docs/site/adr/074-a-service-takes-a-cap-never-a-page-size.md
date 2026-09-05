# ADR 074: A service takes a cap, never a page size

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `074`
- Title: `A service takes a cap, never a page size`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/074-a-service-takes-a-cap-never-a-page-size.yaml`

## Decision

A service list option is called MaxResults and caps the total returned. It is not called Limit, which does not say which behaviour it selects, and it is not called PageSize, which is a behaviour no caller can use. Paging is openapi.PageThrough's, which sizes each request from what is still missing; a caller has no cursor to advance and nothing to do with the window. TestNoServiceOptionIsCalledLimit fails on a field or exported parameter named any of the three. Zero means the service's default cap, not unlimited; a service that can be asked for everything exports an AllResults for it.

## Agent Instructions

Name a new list option MaxResults, field or parameter, and drive it through openapi.PageThrough. Do not add one called Limit or PageSize, and do not hand-roll the walk. A CLI --limit flag is a total and maps straight to MaxResults; paging.Truncate afterwards is then belt and braces rather than the thing that makes the flag work. Unexported helpers may still speak of pages.

## Rationale

This began as a rule about ambiguity. Eleven services capped and eight paged to exhaustion, the field was called Limit in all nineteen, and the wrong guess failed silently in the direction that loses data. Renaming was not enough. Seven --limit flags shipped doing nothing, and in every one the field was honestly called PageSize: the CLI handed a cap to something that took a window, and no naming rule reaches a call site where both halves are named correctly. One of them was worse -- the dashboard's option was already called MaxResults and was already a page size. Removing the second meaning is what closes it. With no page size on the surface, a cap cannot land on one, and the value a caller passes has only one thing it can mean.

## Rejected Alternatives

- `Keep both names and document which is which`: Documentation is what was already missing, and it does not fail a build.
- `Keep PageSize and check the call sites statically instead`: The check would have to know whether a value flowing into a parameter is a cap, which needs type resolution the guard does not have, and would still pass the dashboard.
- `Return a truncated flag alongside the results`: Composes with the envelope's meta.limitReached and is worth having, but it makes truncation observable rather than unambiguous.
