# ADR 050: Frequently-used commands get a shallow alias, and the deep path stays canonical

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `050`
- Title: `Frequently-used commands get a shallow alias, and the deep path stays canonical`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/050-shallow-aliases-for-deep-command-paths.yaml`

## Decision

Where a frequently-used command sits more than three levels deep, or is spelled differently from the well-known gh form, register it a second time under the shorter or more familiar name. The original path stays canonical: it is the one the docs recommend, and the one that names its subject in the path rather than in a flag. Both spellings appear in the generated command reference, because it walks the command tree. Each one's help text must name the other, so the cross-reference works from either end. An alias that points at its canonical path while the canonical page says nothing leaves a reader who arrived the recommended way unable to discover the shorter name at all. Build the alias from a shared constructor, never by copying the body and never by adding the same *cobra.Command value to two parents. A constructor gives each registration its own flag variables and lets it resolve --repo from the tree it actually sits in; a shared value silently couples the two. Every alias carries a test asserting byte-identical output to the canonical path, in the unit suite and in the live suite. That assertion is what makes it an alias rather than a second implementation. A shallower spelling that drops a path segment must move the distinction it carried into a flag, not lose it. bb repo permissions grant takes --group where the deep path had a groups segment.

## Agent Instructions

Prefer the canonical deep path when generating documentation, examples or scripts: it names its subject unambiguously, so a dropped flag cannot silently change which principal it acts on. Either spelling is correct to run. When adding an alias, extract a constructor that both registrations call. Do not register one *cobra.Command under two parents, and do not copy the RunE. Set the cross-reference on both commands in the same change, not just on the alias. Add the equality assertion in the same change. An alias without one is a duplicate waiting to drift. Do not add an alias speculatively. The bar is a command an agent or a person reaches for often enough that a wrong guess is a recurring cost, not merely a path that reads long.

## Rationale

Depth is a specific cost for the primary consumer. An agent discovers a command tree through --help, so every level is a round trip, and an unfamiliar intermediate segment is a name to guess wrong. The wrong invocation that reached docs/site/advanced/dry-run-planning.md was produced exactly this way, before docs-lint existed to catch it (ADR-048). gh caps at three levels. Ten bb commands ran to five or six, all of them under repo settings, and the ones agents reach for most — granting and revoking repository permissions — were among them. Familiarity is the second half. gh spells the pull request diff `gh pr diff`, and the MCP tool is named get_pr_diff, so `bb diff pr` was the odd one out of three names for one operation. Keeping the deep path canonical rather than replacing it is what makes this cheap. Nothing breaks, the reference stays unambiguous about which principal a command acts on, and the alias is free to be terser than a canonical name could responsibly be. The equality tests are the load-bearing part. This repository has repeatedly been bitten by hand-maintained second copies that drifted from their source — the SKILL.md tool table and the ADR-039 tier list among them — and the resolution each time was to delete the copy and point at the source. An alias cannot be deleted that way, so it is pinned instead.

## Rejected Alternatives

- `Flatten the deep paths outright and drop the long spellings`: Breaks every existing script and document, and loses the unambiguous naming. bb repo settings security permissions groups grant says which principal it acts on in the path; bb repo permissions grant --group says it in a flag that is easy to omit. Both spellings existing means the safer one is still available and still documented.
- `Add aliases for every deep path`: The tree has 224 leaf commands and the depth of the ~20 an agent actually reaches for is the problem, not depth as such. Aliasing everything doubles the surface an alias test has to pin and doubles what --help output has to explain, in exchange for shortening paths nobody walks.
- `Keep one *cobra.Command value and add it to both parents`: Fewer lines and immediately wrong. Cobra flag variables are captured per command, so the two registrations would share them, and a persistent --repo would resolve from whichever tree parsed last rather than the tree the caller invoked.
- `Rely on `bb ai` guidance and the generated reference instead of aliases`: Correct for an agent that reads them and useless for one that guesses first, which is the behaviour being paid for. Documentation lowers the cost of a wrong guess; an alias removes it.
