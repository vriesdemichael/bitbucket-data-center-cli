# Local Bitbucket stack

Bitbucket Data Center instance used by the live integration suite.

## No licence key required

The instance is provisioned and licensed by the [Atlassian Plugin SDK](https://developer.atlassian.com/server/framework/atlassian-sdk/).
`atlas-run` resolves the product from Atlassian's public Maven repository and
installs a development licence itself — 3 hours, 12 users, reissued on every
start.

That means no `BITBUCKET_LICENSE_KEY`, no `.env` file and no Atlassian account.
The stack runs identically on a fork, on any contributor's machine, and in CI,
which is why the live suite is no longer restricted to pull requests from this
repository.

A full live suite run takes about five minutes, so the 3-hour window only
matters for long local sessions, and the instance handles those itself.

### The instance stops itself before its licence runs out

Bitbucket keeps reporting `RUNNING` after the licence expires and only refuses
writes, so an expired instance fails the live suite in ways that look like
product bugs.

The container therefore stops itself when the licence is 2h45m old. A stopped
instance holds no memory, and `task test:live` starts it again before it runs,
with a new licence; `task stack:up` does the same on its own.

Until the stop, the healthcheck reports an instance that age as `unhealthy`, and
the live suite refuses to start against one with less than ten minutes left.
`task stack:restart` issues a fresh licence straight away.

CI never reaches the limit: each run creates the container from scratch.

## Usage

```bash
task stack:up        # start this checkout's instance and enable basic auth
task stack:status    # this instance, how long before it stops itself, every local instance
task stack:logs
task stack:down
task stack:reset     # tear down this instance and delete its Maven cache volume
task stack:prune     # remove instances whose worktree no longer exists
```

`task test:live` runs `task stack:up` first, so starting the stack by hand is
optional. In the main checkout the instance is served at `http://localhost:7990`
with admin credentials `admin` / `admin`.

### One instance per checkout

Each checkout has its own instance: its own compose project, container, Maven
cache volume and licence, so a restart or a fixture purge in one git worktree
leaves the others alone. The main checkout keeps `http://localhost:7990`. A
linked worktree gets ports Docker assigns, which change each time its instance
starts; `task stack:up` writes the current URL to `.tmp/bitbucket.env`, where the
live suite reads it.

A worktree's first start downloads about 360MB on top of what the image already
holds. `task stack:up` removes instances whose worktree is gone before it
starts, and refuses to start a fifth running instance (`BB_STACK_MAX`), since
each is a Bitbucket JVM of about 6GB.

## Version

The product version is pinned in exactly one place: the base image tag in
[`harness/Dockerfile`](harness/Dockerfile). Dependabot manages it.

`atlas-run` reads the version back out of the image at build time, so bumping
the tag moves the whole harness together and there is no second constant to keep
in sync.

### Running more than one version

Nothing here assumes a single instance. The host ports are overridable, so a
second version can run alongside this one:

```bash
BITBUCKET_HOST_PORT=8990 BITBUCKET_SSH_HOST_PORT=8999 \
  docker compose -p bitbucket-10-2 -f docker/compose.yml up -d --wait
```

For a genuinely different product version you also need a different base image
tag. The `FROM` line is deliberately a literal rather than a build argument,
because Dependabot tracks literal tags reliably and argument-based ones less so.
Turning it into an argument (or adding a second harness directory) is a small
change when multi-version support is actually wanted.

## Why the base image is the official Bitbucket image

The JVM and git must both fall inside windows the product accepts, and those
windows are narrow and version-specific. For Bitbucket 10.4.2:

- Java **21** is required (the webapp is compiled to class file version 65)
- git must be **>= 2.42** and **< 2.55**, and **2.48, 2.51, 2.52 and 2.53 are
  additionally rejected** for "critical regressions which break core
  functionality"

A rejected git is the dangerous case: the instance logs a clean
`Started BitbucketServerApplication` and only afterwards parks in `ERROR` when
the Mesh sidecar fails to wire up. Building on the official product image means
both come from Atlassian and are correct by construction for the pinned version.
`harness/Dockerfile` also asserts them at build time so that a future base-image
change fails the build rather than the suite.

## Database

The SDK supplies the instance's embedded database, so there is no separate
Postgres service. The live suite exercises the REST API, where behaviour is
equivalent. Anything that needs to characterise Postgres-specific behaviour
should stand up its own instance and say so explicitly rather than relying on
this stack.

## Licensing

Bitbucket Data Center is proprietary Atlassian software. This stack uses the
development licence that the Atlassian Plugin SDK issues for exactly this
purpose; use it accordingly and within Atlassian's terms.
