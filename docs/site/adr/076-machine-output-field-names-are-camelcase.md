---
search:
  boost: 0.3
---

# ADR-076: Machine output field names are camelCase, matching the Bitbucket API

Every field name in machine output is camelCase, including the document's own: `bbVersion`, `limitReached`, `exitCode`. A single word is already camelCase. A field mirroring an upstream Bitbucket object keeps the upstream name exactly, so a reader comparing bb output against the Atlassian API documentation sees the same word. Diverge only where the upstream name would mislead a reader who does not know Bitbucket's internal vocabulary; then name the field for what it returns, and say in its description what it corresponds to upstream. A name that is merely unfamiliar is no reason to rename; one that describes an implementation detail the caller does not have is. The rule covers everything bb writes to stdout under `--json` or `--yaml`, not only command payloads: the dry-run preview and the keys inside `error.details` are output too. Where a human rendering prints key=value pairs, the key is the field name, so a reader comparing the two renderings sees one name for one thing. Input flags stay kebab-case, which is what a CLI reader expects. Structured log lines on stderr are a separate surface and keep their own names.

Name a new output field in camelCase. Do not introduce snake_case, and do not publish a Go struct without JSON tags: an untagged field publishes its Go name, which is PascalCase. When a field mirrors an upstream object, copy the upstream name rather than improving it. When it does not mirror anything, name it for what it holds. `TestEveryPublishedFieldIsCamelCase` checks every declared result type. It does not check the keys of an open map, which are Bitbucket's own.

The Bitbucket Data Center API names its fields in camelCase and has no snake_case, so matching it gives every pass-through field an exact correspondence with the API documentation and costs only bb's own fields a convention. A payload that carries displayId beside default_branch makes a caller memorise which rule each field followed instead of predicting it.

## Not chosen

- **snake_case throughout**: Defensible, and common in JSON, but it diverges from a consistent upstream. Every pass-through field would need a translation that exists only to change the spelling, and a reader cross-referencing Atlassian's documentation would do one mental transform per field.
- **Upstream names for pass-through fields, snake_case for bb's own**: It sounds principled and reads as an accident: a caller cannot tell which rule a field followed without knowing whether it came from Bitbucket, so the shape has to be memorised rather than predicted.
- **Leave untagged Go structs as they are**: A consumer reading repository.projectKey gets nothing when the payload says ProjectKey.
