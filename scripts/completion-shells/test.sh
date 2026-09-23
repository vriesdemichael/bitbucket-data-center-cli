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

arch="${BB_COMPLETION_ARCH:-amd64}"

mkdir -p .tmp
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o .tmp/bb-completion-shells ./cmd/bb

# The .deb and .rpm, built the way release-artifacts.yml builds them: the same
# nfpm.yaml, the completion scripts generated the same way, and the nfpm the
# release pins, read from that workflow so the two cannot drift apart.
packages=.tmp/bb-completion-packages
rm -rf "$packages"
mkdir -p "$packages/completions"
for shell in bash zsh fish; do
	CGO_ENABLED=0 go run ./cmd/bb completion "$shell" >"$packages/completions/bb.$shell"
done
if ! nfpm="$(grep -o -m 1 'github.com/goreleaser/nfpm/v2/cmd/nfpm@v[0-9.]*' .github/workflows/release-artifacts.yml)"; then
	echo "release-artifacts.yml no longer installs nfpm by that path; build the packages the way it does now" >&2
	exit 1
fi
for packager in deb rpm; do
	PKG_ARCH="$arch" PKG_VERSION=0.0.0 PKG_BINARY=.tmp/bb-completion-shells PKG_COMPLETIONS="$packages/completions" \
		go run "$nfpm" package --config scripts/nfpm.yaml --packager "$packager" --target "$packages/bb.$packager" >/dev/null
done

docker build --quiet --tag bb-completion-shells:local scripts/completion-shells >/dev/null

# Git Bash rewrites anything that looks like a Unix path before Docker sees it,
# and hands Docker a POSIX path it cannot mount; pwd -W is the Windows form it
# can. Elsewhere pwd is already what Docker wants.
host_root="$root"
if pwd -W >/dev/null 2>&1; then
	host_root="$(pwd -W)"
fi

status=0

# The .rpm goes to Fedora, whose zsh reads a different directory from
# Debian's; run.sh installs the .deb in the image beside the shells.
MSYS_NO_PATHCONV=1 docker run --rm \
	-v "${host_root}/${packages}:/packages:ro" \
	-v "${host_root}/scripts/completion-shells:/scripts:ro" \
	fedora:41 sh /scripts/rpm.sh || status=1

MSYS_NO_PATHCONV=1 docker run --rm \
	-v "${host_root}/.tmp/bb-completion-shells:/usr/local/bin/bb:ro" \
	-v "${host_root}/${packages}:/packages:ro" \
	-v "${host_root}/scripts/completion-shells:/scripts:ro" \
	bb-completion-shells:local || status=1

exit "$status"
