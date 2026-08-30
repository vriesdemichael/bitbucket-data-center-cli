# Bitbucket Data Center test instance, licensed by the Atlassian Plugin SDK.
#
# The SDK provisions the product from Atlassian's public Maven repository and
# installs a short-lived development licence automatically (3 hours, 12 users,
# reissued on every start). No licence key and no Atlassian account are
# required, so this stack runs unchanged on forks and on any contributor's
# machine.
#
# Why the base image is the official Bitbucket image rather than a plain JDK:
# the JVM and git must both fall inside the window the product accepts, and
# those windows are narrow and version-specific. Bitbucket 10.4.2 requires
# Java 21, requires git >= 2.42, rejects git >= 2.55, and additionally rejects
# 2.48, 2.51, 2.52 and 2.53 for "critical regressions". A rejected git is
# especially nasty: the instance logs a clean "Started BitbucketServerApplication"
# and only then parks in ERROR when the Mesh sidecar fails. Inheriting both from
# the product image means they are correct by construction for whichever version
# is pinned below.
#
# The image tag is therefore the single version pin for the whole harness.
# Dependabot manages it, and the product version is read back out of the image
# at build time so nothing has to be kept in sync by hand.
FROM atlassian/bitbucket:10.4.2

ENV DEBIAN_FRONTEND=noninteractive
ENV JAVA_HOME=/opt/java/openjdk

RUN apt-get update \
    && apt-get install -y --no-install-recommends unzip \
    && rm -rf /var/lib/apt/lists/*

# Single source of truth for the product version: the image itself.
RUN sed -n 's/^build\.version[[:space:]]*=[[:space:]]*//p' \
        /opt/atlassian/bitbucket/app/WEB-INF/classes/build.properties \
        | tr -d '[:space:]' > /bitbucket-version \
    && test -s /bitbucket-version \
    && echo "Bitbucket version: $(cat /bitbucket-version)"

# Fail the build, not the test suite, if the toolchain ever drifts outside the
# product's supported window. This should never fire while the base image is the
# official one — it exists so that a future base change surfaces here instead of
# as an unexplained ERROR state mid-run.
RUN java -version 2>&1 | head -1 \
    && git --version \
    && java -version 2>&1 | grep -q '"21' \
    && git --version | grep -qvE ' 2\.(48|51|52|53)\.'

RUN mkdir -p /opt/atlassian-sdk \
    && curl -fsSL -o /tmp/sdk.tgz \
        "https://marketplace.atlassian.com/download/plugins/atlassian-plugin-sdk-tgz" \
    && tar -xzf /tmp/sdk.tgz -C /opt/atlassian-sdk --strip-components=1 \
    && rm /tmp/sdk.tgz

ENV ATLAS_HOME=/opt/atlassian-sdk
ENV PATH="/opt/atlassian-sdk/bin:${PATH}"

# Maven writes download progress as carriage-return redraws meant for a
# terminal. A CI log has no terminal to redraw, so every update becomes its own
# line: the stack step emitted about 8,000 of them -- "Progress (1): 12/14 MB"
# over and over -- out of roughly 9,900 lines total, burying the two or three
# lines anybody reads.
#
# Set on the image rather than per command so it covers the builds below and
# atlas-run at container start. MAVEN_ARGS is read by Maven 3.9+, which the SDK
# bundles.
#
# MAVEN_OPTS would silence the same output through an slf4j property and would
# reach wrappers that do not forward MAVEN_ARGS, but setting it here replaces
# whatever heap flags atlas-run relies on for the product JVM. That trade --
# risking an out-of-memory four minutes into a CI run to tidy a log -- is not
# worth taking.
ENV MAVEN_ARGS="--batch-mode --no-transfer-progress"

WORKDIR /work

# AMPS drives everything from a plugin project. We never build or install a
# plugin — the project exists only to give atlas-run a pom to read. Scaffolding
# it at build time keeps startup deterministic and offline-ish.
RUN atlas-create-bitbucket-plugin \
        --artifact-id harness \
        --group-id com.atlassian.bitbucket.harness \
        --version 1.0 \
        --package com.atlassian.bitbucket.harness \
        --non-interactive

# Warm the largest artifact so a cold start is not dominated by one download.
RUN /opt/atlassian-sdk/apache-maven-3.9.8/bin/mvn -q --batch-mode --no-transfer-progress \
        -gs /opt/atlassian-sdk/apache-maven-3.9.8/conf/settings.xml \
        dependency:get \
        -Dartifact="com.atlassian.bitbucket.server:bitbucket-webapp:$(cat /bitbucket-version):war" \
    || echo "WARN: could not pre-fetch bitbucket-webapp; it will download on first start"

COPY start-bitbucket.sh /usr/local/bin/start-bitbucket.sh
RUN chmod +x /usr/local/bin/start-bitbucket.sh

EXPOSE 7990 7999

# The base image ships an entrypoint that starts the bundled Bitbucket. We run
# the SDK-provisioned instance instead, so clear it.
ENTRYPOINT []
CMD ["/usr/local/bin/start-bitbucket.sh"]
