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
matters for long local sessions. `task stack:restart` issues a fresh licence.

### Stale instances are caught, not silently used

`docker compose up -d` reuses a running container rather than recreating it, and
Bitbucket keeps reporting `RUNNING` after the licence expires. Left alone, that
would mean the live suite running against a dead instance and failing in ways
that look like product bugs.

The healthcheck therefore also fails once the licence is within ~15 minutes of
expiry, so an aged container is reported `unhealthy` and `task stack:up` exits
non-zero instead of proceeding:

```
container bb-bitbucket is unhealthy
```

The fix is always `task stack:restart`, which issues a fresh licence.

CI never hits this — each run creates the container from scratch.

## Usage

```bash
task stack:up        # start (first run downloads ~800MB of product artifacts)
task stack:status
task stack:logs
task stack:down
task stack:reset     # tear down and delete the Maven cache volume
```

Then enable basic authentication, which Bitbucket 10 disables by default even
once the instance reports `RUNNING`:

```bash
bash scripts/bootstrap-bitbucket.sh http://localhost:7990 admin admin
```

Bitbucket is served at `http://localhost:7990` with admin credentials
`admin` / `admin`.

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
