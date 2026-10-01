---
search:
  boost: 0.3
---

# ADR-078: A dry run's tier is the weakest of its checks, not a label the author writes

Every item a dry run checks states how its prediction was reached, as one of three tiers: server-validated, where Bitbucket answered the exact question through its own dry-run endpoint or an equivalent authoritative call; preconditions-checked, where the caller's permission and the current state were both fetched and the preconditions that decide the operation were evaluated; and predicted, where the answer was derived from partial state. The verdict reports the weakest tier among its items. An item that states no tier is predicted, and so is a preview with nothing checked. A command that only reads runs, and its verdict is server-validated. A verdict found as a failure is server-validated when Bitbucket gave the answer, and preconditions-checked when bb found it before asking.

Each command's profile in `dryRunProfiles` declares the strongest tier its preview reaches when every check can be made, and its `--dry-run` help line and `--describe` are generated from that declaration. `TestEveryStatefulProfileDeclaresItsTier` fails on a stateful profile that names none. A preview may report less than its declared tier and never more, and the live suite fails one that reports more.

When adding or changing a preview, set `Tier` on each `dryrunpreview.Item`. Claim preconditions-checked only when the code evaluated the preconditions that decide the operation, not merely when it fetched something: a delete that reads the object but never checks whether it is empty is predicted, and says why in a comment beside the item. When a check cannot be made at runtime, as on a Bitbucket that does not report whether a pull request can merge, lower the tier for that answer rather than report it as checked.

A label typed beside a prediction is only as honest as its author is modest, and nothing relates it to what the code did, so the operation where being wrong costs most can claim most from a single state field. With the tier as the input and the verdict's claim computed from it, an overclaim cannot be written. The tier also tells a reader whether "would apply" was confirmed by Bitbucket or inferred from a state field.

## Not chosen

- **Infer the tier statically from what each call site does**: A permission pre-flight plus a non-constant predicted action classifies most sites correctly and over-credits the rest, because fetching state is not the same as evaluating the preconditions. It makes a good first guess and a bad contract.
