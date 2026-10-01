---
search:
  boost: 0.3
---

# ADR-074: A service takes a cap, never a page size

A service list option is called `MaxResults` and caps the total returned. It is not called `Limit`, which does not say which behaviour it selects, and it is not called `PageSize`, which is a behaviour no caller can use. Paging is `openapi.PageThrough`'s, which sizes each request from what is still missing; a caller has no cursor to advance and nothing to do with the window. `TestNoServiceOptionIsCalledLimit` fails on a field or exported parameter named for either. Zero means the service's default cap, not unlimited; a service that can be asked for everything exports an `AllResults` for it. A result that reached its cap says so, whatever it is read through: `meta.limitReached` under `--json`, `limit_reached` from an MCP list tool, and a line on stderr from `paging.Hint` in text. It means there may be more, not that there is. `TestACappedListingSaysSoIsEnforced` and `TestEveryLimitedToolSaysWhenItStopped` fail on a surface that stays silent.

Name a new list option `MaxResults`, field or parameter, and drive it through `openapi.PageThrough`. Do not add one called `Limit` or `PageSize`, and do not hand-roll the walk. A CLI `--limit` flag is a total and maps straight to `MaxResults`; `paging.Truncate` afterwards is then belt and braces rather than the thing that makes the flag work. Unexported helpers may still speak of pages. A new list command passes `paging.LimitReached` to `WriteJSONList` and calls `paging.Hint` before its text output returns. A new MCP list tool returns its collection through `capped` and sets `limit_reached` from it. A command that takes `--limit` only to find one thing rather than to return a page says so in its `RunE` with a `limit-not-reported:` comment giving the reason.

A name with two meanings fails silently, in the direction that loses data. A correct name is not enough either: a CLI that hands a cap to an option that takes a window ships a `--limit` that does nothing, and no naming rule reaches a call site where both halves are named correctly. With no page size on the surface, a cap cannot land on one, and the value a caller passes has only one thing it can mean.

## Not chosen

- **Keep both names and document which is which**: Documentation does not fail a build.
- **Keep PageSize and check the call sites statically instead**: The check would have to know whether a value flowing into a parameter is a cap, which needs type resolution the guard does not have.
- **Keep PageSize and have each service report whether it truncated**: It makes a page size observable rather than removing it. The signal a caller reads comes from the cap itself -- meta.limitReached, limit_reached, the text hint -- and needs no second meaning to exist.
