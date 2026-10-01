---
search:
  boost: 0.3
---

# ADR-004: Live integration tests as primary correctness gate

Behaviour that depends on Bitbucket is proven against a real Bitbucket, in the live suite under tests/integration/live. What the server does is authoritative; its documentation and the OpenAPI specification are advisory. No contract suite, recorded fixture or mock server stands in for it, and ADR-079 says what a unit test may do instead.

A live test reads back every value it wrote, with a separate GET after the write, and writes a value other than the default. A 2xx proves nothing: Bitbucket accepts a field it does not know and answers success, and a default reads back the same whether or not the write landed. A command counts as reached only when a live test runs it for real and asserts on the result; command reach (ADR-065) does not count a run under `--dry-run`, a result thrown away, or a test that skips when the call fails.

When a change does anything beyond parsing or local validation, add or update a live test against the local stack. To fix a bug, reproduce it with a live test first.

Bitbucket's documentation is incomplete and sometimes wrong at the edges, and permissions and server-managed state behave in ways it does not describe. A test built from what its author believes about the API agrees with the code whenever that belief is the bug; only the server can disagree.

## Not chosen

- **Contract tests or recorded fixtures**: They encode what the server was believed or seen to do, and keep passing when it does something else.
- **Implement from the documentation without live verification**: Too risky, given how far the documentation is from the server at the edges.
