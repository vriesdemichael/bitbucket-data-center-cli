---
search:
  boost: 0.3
---

# ADR-068: The vendored API reference is derived from the harness release

Atlassian's published OpenAPI document is the single external API reference, vendored at docs/reference/atlassian/bitbucket-openapi.json so generation and review need no network. Its release is derived, never pinned: tools/openapi-spec reads the harness base image tag (ADR-042) and fetches the document Atlassian publishes for that major and minor. The vendored file carries no release in its name. `task openapi:refresh` vendors the matching document, and `task openapi:verify` fails when the vendored document and the harness have parted ways, naming the refresh command. It runs in the unit-tests CI job and before every push. The document is reference, not proof of behaviour: behaviour is established by the live suite (ADR-004), and a place where the document is wrong is registered and pinned by a test (ADR-028).

Read endpoints, payload shapes and status codes from the vendored document. Do not consult another release's documentation. Do not pin an API release anywhere: not in a filename, a Taskfile variable or prose. When the harness tag changes, run `task openapi:refresh` in the same change and regenerate the models and the client; models:verify and client:verify say whether you did. On a refresh, read the diff of docs/quality/generated-operation-paths.json before accepting it.

A release in a filename or a variable is a second copy of a fact the harness tag holds, and copies do not move together; deriving removes the copy rather than policing it. The bump and the refresh go in one change because between them the live suite tests one release while the generated client takes its shape from another. Atlassian re-points operationIds between releases, and an operation that now names a different endpoint still compiles and passes the unit tests while the CLI calls the wrong URL.

## Not chosen

- **Keep the release in the vendored filename**: Readable, and a second copy of the release. The document states its own version, and openapi:verify checks it against the harness on every run.
- **Track the newest published specification rather than the harness release**: A reference ahead of the harness describes endpoints the live suite cannot exercise.
- **Refresh on a schedule rather than alongside the bump**: Would vendor a document for a release the harness does not run, failing the gate on every unrelated pull request until someone reconciled the two.
