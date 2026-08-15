#!/usr/bin/env bash
# Start the SDK-provisioned Bitbucket instance.
#
# atlas-run resolves the product from Atlassian's public Maven repository and
# installs a development licence itself, so BITBUCKET_LICENSE_KEY is not used
# anywhere in this stack.
#
# Two flags are load-bearing:
#
#   --version        read from the image so it always matches the base image tag
#   -Dcontext.path=  serve at / rather than /bitbucket
#
# On the second: AMPS documents `--context-path ROOT` for root serving, but that
# is a Tomcat-era convention. Bitbucket 10 runs on Spring Boot's embedded server,
# which rejects it with "ContextPath must start with '/' and not end with '/'".
# An empty context path is the value that works, and it matters because the live
# suite asserts on unprefixed endpoint paths.
set -euo pipefail

BB_VERSION="$(cat /bitbucket-version)"

echo "==> Bitbucket ${BB_VERSION}"
echo "==> $(java -version 2>&1 | head -1)"
echo "==> $(git --version)"
echo "==> Licence: Atlassian Plugin SDK development licence (3h, 12 users, reissued each start)"

cd /work/harness

# Record when the licence was issued so the healthcheck can retire the instance
# before it expires. Without this, `docker compose up -d` happily reuses a
# container that has been running for days: /status still reports RUNNING on an
# expired licence, so the live suite would run against a dead instance and fail
# in ways that look like product bugs.
date +%s > /tmp/licence-issued-at

# atlas-run reads stdin and treats EOF as a shutdown request, so the container
# must be run with stdin open and a TTY attached (compose: stdin_open + tty).
# Without them the instance starts, immediately runs amps:stop, and exits 0 —
# which looks like a successful run that produced nothing.
exec atlas-run \
    --product bitbucket \
    --version "${BB_VERSION}" \
    -Dcontext.path=
