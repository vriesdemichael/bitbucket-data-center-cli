#!/usr/bin/env bash
# Runs the shell completion checks in their image, the same way everywhere:
# `task completion:shells` calls this on a developer's machine and in CI.
#
# bb is built for Linux into .tmp and mounted, rather than baked into the
# image, so the image -- four shells and nothing of bb's -- builds once and is
# reused, and a change to bb costs a Go build, not an image build.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"

mkdir -p .tmp
GOOS=linux GOARCH="${BB_COMPLETION_ARCH:-amd64}" CGO_ENABLED=0 go build -o .tmp/bb-completion-shells ./cmd/bb

docker build --quiet --tag bb-completion-shells:local scripts/completion-shells >/dev/null

# Git Bash rewrites anything that looks like a Unix path before Docker sees it,
# and hands Docker a POSIX path it cannot mount; pwd -W is the Windows form it
# can. Elsewhere pwd is already what Docker wants.
host_root="$root"
if pwd -W >/dev/null 2>&1; then
	host_root="$(pwd -W)"
fi

MSYS_NO_PATHCONV=1 docker run --rm \
	-v "${host_root}/.tmp/bb-completion-shells:/usr/local/bin/bb:ro" \
	-v "${host_root}/scripts/completion-shells:/scripts:ro" \
	bb-completion-shells:local
