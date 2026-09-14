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

# Record when the licence was issued. The healthcheck and the live suite read
# it to tell how long the instance has left: /status keeps reporting RUNNING on
# an expired licence, so nothing else can.
date +%s > /tmp/licence-issued-at

# Stop the container before the licence runs out. A stopped instance holds no
# memory, whether or not anyone is still working, and `task stack:up` -- which
# `task test:live` runs first -- starts it again with a new licence. Left
# running, it would keep answering RUNNING while refusing every write.
#
# Process 1 first, then everything else. atlas-run becomes process 1 and is a
# shell script that runs Maven as a child, and a shell as process 1 ignores
# SIGTERM, so it exits only once Maven and the product JVM are gone. The second
# kill reaches this subshell too, which is why it comes last.
retire_after="${BB_LICENCE_RETIRE_SECONDS:?set in docker/harness/Dockerfile}"
(
  sleep "${retire_after}"
  echo "==> SDK licence is $(( retire_after / 60 )) minutes old; stopping so the next start issues a new one"
  kill -TERM 1 2>/dev/null || true
  kill -TERM -1 2>/dev/null || true
) &

# atlas-run reads stdin and treats EOF as a shutdown request, so the container
# must be run with stdin open and a TTY attached (compose: stdin_open + tty).
# Without them the instance starts, immediately runs amps:stop, and exits 0 —
# which looks like a successful run that produced nothing.
exec atlas-run \
    --product bitbucket \
    --version "${BB_VERSION}" \
    -Dcontext.path=
