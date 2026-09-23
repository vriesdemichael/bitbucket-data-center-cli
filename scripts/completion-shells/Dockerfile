# Four shells in one image, so scripts/completion-shells/run.sh behaves the same
# on a developer's machine as in CI: CI runs this image too, rather than
# installing the shells on its runner, which would be a second environment to
# keep in step with the first.
#
# PowerShell is the base because it is the one shell the distribution does not
# package; the other three come from Debian.
FROM mcr.microsoft.com/powershell:lts-debian-12

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash-completion zsh fish tmux ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# bb itself is mounted at run time, built for this platform by the task, and so
# is the .deb run.sh installs, so the image does not need rebuilding for every
# change to bb.
ENTRYPOINT ["bash", "/scripts/run.sh"]
