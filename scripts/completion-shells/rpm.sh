#!/bin/sh
# The .rpm, installed in Fedora: each completion file lands where Fedora's
# shells search, its script succeeds, and the bb it installs runs. zsh is the
# one that differs from Debian -- site-functions here, where Debian's zsh reads
# vendor-completions -- which is why the package is checked on Fedora at all.
#
# run.sh installs the .deb beside the shells themselves and drives them; this
# reads what rpm says it installed. test.sh mounts the packages at /packages.
set -u

failures=0

fail() {
	printf 'FAIL  rpm   %s\n' "$1"
	printf '%s\n' "$2" | sed -e '/^[[:space:]]*$/d' -e 's/^/        | /'
	failures=$((failures + 1))
}

if ! output="$(rpm -i /packages/bb.rpm 2>&1)"; then
	fail "rpm -i failed" "$output"
	exit 1
fi
case "$output" in
*"bb ai skill install --global"*) ;;
*) fail "the post-install script did not name the skill install" "$output" ;;
esac

installed="$(rpm -ql bb)"
for wanted in \
	/usr/bin/bb \
	/usr/share/bash-completion/completions/bb \
	/usr/share/zsh/site-functions/_bb \
	/usr/share/fish/vendor_completions.d/bb.fish; do
	if ! printf '%s\n' "$installed" | grep -qxF "$wanted"; then
		fail "$wanted is not installed" "$installed"
	fi
done
if printf '%s\n' "$installed" | grep -qF vendor-completions; then
	fail "the .deb's zsh directory is in the .rpm" "$installed"
fi

if ! version="$(/usr/bin/bb --version 2>&1)"; then
	fail "the installed bb does not run" "$version"
fi

if [ "$failures" -gt 0 ]; then
	printf '\n%d .rpm check(s) failed.\n' "$failures"
	exit 1
fi

printf 'The .rpm installs completion where Fedora looks for it.\n'
