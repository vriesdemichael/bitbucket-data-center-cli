---
search:
  boost: 0.3
---

# ADR-043: Provision the live test instance with the Atlassian Plugin SDK

The Bitbucket instance the live suite runs against is provisioned by the Atlassian Plugin SDK (atlas-run), which resolves the product from Atlassian's public Maven repository and installs a development licence itself. The stack needs no licence key, no .env file and no Atlassian account, so it runs the same on a fork, on any contributor's machine and in CI, and the live-tests job runs on every pull request, forks included. The harness image in docker/harness is built from the official Bitbucket product image, so the JVM and git come with the product rather than being chosen separately, and its tag is the one place the release under test is pinned (ADR-042). Each checkout runs an instance of its own through scripts/stack.sh, and `task test:live` starts it before it runs.

Do not add a BITBUCKET_LICENSE_KEY, a licence secret or any other credential to the stack definition or to CI. Do not gate the live-tests job on where a pull request comes from, and do not let CI Complete accept a skipped live-tests result: a skip means the correctness gate did not run. Do not replace the harness base image with a plain JDK image. The SDK licence lasts three hours from the start of the process, and Bitbucket goes on reporting RUNNING after it expires, so the instance stops itself before the licence runs out and the compose healthcheck fails on a licence past that age. Keep both when changing the stack; without them a stale instance fails the suite as though the product were broken.

A gate that needs a secret cannot run on a fork's pull request and cannot be reproduced by a contributor, so the project's primary correctness gate would give an outside change no signal. The SDK issues a development licence for exactly this purpose. Java and git must both fall inside windows the product accepts, which are narrow and change between releases, and a rejected git gives an instance that logs a clean startup and then parks in ERROR. Inheriting both from the product image keeps them right for whichever release is pinned.

## Not chosen

- **A licensed container stack and a repository licence secret**: Fork pull requests cannot read the secret, so the primary correctness gate cannot run on outside contributions, and contributors cannot reproduce it.
- **Record and replay HTTP fixtures instead of running a real instance**: Fixtures cover only what was already recorded, so every new command or changed call sequence needs a live instance anyway, and a replay freezes behaviour where the live suite exists to discover it.
- **A plain JDK image with a pinned git**: Three versions chosen independently, each of which must stay inside a narrow window that changes between releases. The product image makes them right by construction and leaves one pin.
- **Postgres to match production deployments**: The SDK supplies an embedded database, and the live suite exercises the REST API, where behaviour is the same. Anything that characterises database-specific behaviour stands up its own instance and says so.
