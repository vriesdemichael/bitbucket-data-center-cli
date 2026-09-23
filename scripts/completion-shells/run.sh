#!/usr/bin/env bash
# Drives bb's generated completion scripts in real shells and asserts on what
# the terminal shows.
#
# Every Go test of completion reads the protocol -- the candidates and the
# directive bb prints. That output can be exactly right while the shell does
# something wrong with it: Cobra's PowerShell script throws at the prompt on an
# empty answer, its fish script puts a space after a list element whose
# description ends in a full stop, and one sentence repeated beside every value
# is noise in bash and fish that zsh hides. None of that is visible to anything
# that does not look at a terminal. This looks.
#
# Nothing here needs Bitbucket. The assertions are about how each shell treats
# the protocol, so the instance is one that refuses every connection, and the
# values come from the slots that answer without one.
#
# Expects bb, bash with bash-completion, zsh, fish, tmux and pwsh on PATH; the
# Dockerfile beside this builds exactly that. `task completion:shells` runs it.
set -uo pipefail

work="$(mktemp -d)"
mkdir -p "$work/SENTINEL-DIR" "$work/home"
touch "$work/SENTINEL-FILE"

# Port 9 is reserved as discard: nothing listens, and the refusal is
# immediate, so a server-backed slot answers with nothing, promptly.
export BITBUCKET_URL=http://127.0.0.1:9
export BITBUCKET_TOKEN=not-a-real-token
export BB_DISABLE_STORED_CONFIG=1
export HOME="$work/home"

failures=0

report() {
	local shell=$1 case=$2 problem=$3 screen=$4
	printf 'FAIL  %-5s %s: %s\n' "$shell" "$case" "$problem"
	printf '%s\n' "$screen" | sed -e '/^[[:space:]]*$/d' -e 's/^/        | /'
	failures=$((failures + 1))
}

# press types a line, presses Tab twice -- the second press is what makes bash
# list several matches instead of completing their common prefix -- and prints
# the screen as the terminal drew it.
press() {
	local session=$1 line=$2
	tmux send-keys -t "$session" -l "$line"
	tmux send-keys -t "$session" Tab
	sleep 1
	tmux send-keys -t "$session" Tab
	sleep 1.5
	tmux capture-pane -p -t "$session"
	reset_line "$session"
}

reset_line() {
	tmux send-keys -t "$1" C-u
	tmux send-keys -t "$1" -l "clear"
	tmux send-keys -t "$1" Enter
	sleep 0.5
}

expect_in() {
	local shell=$1 case=$2 screen=$3
	shift 3
	for wanted in "$@"; do
		if ! grep -qF -- "$wanted" <<<"$screen"; then
			report "$shell" "$case" "expected \"$wanted\" on screen" "$screen"
			return
		fi
	done
}

expect_absent() {
	local shell=$1 case=$2 screen=$3
	shift 3
	for unwanted in "$@"; do
		if grep -qF -- "$unwanted" <<<"$screen"; then
			report "$shell" "$case" "did not expect \"$unwanted\" on screen" "$screen"
			return
		fi
	done
}

check_shell() {
	local shell=$1 session="completion-$1"

	tmux new-session -d -s "$session" -x 120 -y 30 -c "$work" "${@:2}"
	sleep 3

	local screen

	# An enum flag completes the values it validates against.
	screen="$(press "$session" 'bb pr list --state ')"
	expect_in "$shell" "enum values" "$screen" open closed all

	# Each value describes itself. One sentence on every value is what bash and
	# fish printed four times across two lines.
	screen="$(press "$session" 'bb --log-level ')"
	expect_in "$shell" "distinct descriptions" "$screen" "failures only" "and every request"

	# A server-backed slot with nothing to offer offers nothing -- not the
	# working directory, which is what the shell does unless told otherwise.
	screen="$(press "$session" 'bb pr merge --repo PRJ/nothing ')"
	expect_absent "$shell" "no candidates, no files" "$screen" "SENTINEL-FILE" "Completion ended with directive"

	# The slots that do take a local path still get the shell's own answer.
	screen="$(press "$session" 'bb --ca-file ')"
	expect_in "$shell" "a file slot offers files" "$screen" "SENTINEL-FILE"

	screen="$(press "$session" 'bb repo clone PRJ/repo ')"
	expect_in "$shell" "a directory slot offers directories" "$screen" "SENTINEL-DIR"
	# Cobra's fish script does not filter to directories -- it says so, and
	# falls back to "full file completion instead" -- so fish offers files
	# here as well. A narrower answer than asked for would be a regression;
	# this one is fish's, and known.
	if [ "$shell" != fish ]; then
		expect_absent "$shell" "a directory slot offers no files" "$screen" "SENTINEL-FILE"
	fi

	# One element of a comma list keeps the cursor against it, so the next
	# element can follow the comma. A space here is a list the next word breaks.
	tmux send-keys -t "$session" -l 'bb ai mcp serve --tools list_t'
	tmux send-keys -t "$session" Tab
	sleep 1.5
	tmux send-keys -t "$session" -l ',X'
	sleep 0.5
	screen="$(tmux capture-pane -p -t "$session")"
	expect_in "$shell" "no space after a list element" "$screen" "list_tags,X"
	reset_line "$session"

	tmux kill-session -t "$session"
}

cat >"$work/bashrc" <<'RC'
source /usr/share/bash-completion/bash_completion
source <(bb completion bash)
PS1='$ '
RC

mkdir -p "$work/zdotdir"
cat >"$work/zdotdir/.zshrc" <<'RC'
autoload -Uz compinit && compinit -u
source <(bb completion zsh)
PROMPT='$ '
RC

mkdir -p "$work/home/.config/fish"
cat >"$work/home/.config/fish/config.fish" <<'RC'
set -g fish_greeting ''
bb completion fish | source
function fish_prompt; echo -n '$ '; end
RC

check_shell bash bash --rcfile "$work/bashrc" -i
check_shell zsh env ZDOTDIR="$work/zdotdir" zsh -i
check_shell fish fish -i

# PowerShell is checked through CommandCompletion.CompleteInput, the call its
# own line editor makes on Tab. Its stderr is kept apart: Cobra's "Completion
# ended with directive" line reached the prompt there, and nowhere else.
here="$(cd "$(dirname "$0")" && pwd)"
if ! (cd "$work" && pwsh -NoProfile -File "$here/check.ps1" 2>"$work/pwsh.stderr"); then
	failures=$((failures + 1))
fi
if grep -qF "Completion ended with directive" "$work/pwsh.stderr"; then
	report pwsh "stderr" "a completion wrote to the terminal" "$(cat "$work/pwsh.stderr")"
fi

# bb completion install, in shells with nothing else set up: each starts from
# only what the shell reads by itself, so a completion that appears came from
# the setup, and one that goes away went with bb completion remove. The user's
# setup lives under a home of its own; the one for every user in the system
# directories, which this container, running as root, may write.
plain_home() {
	local home=$1
	mkdir -p "$home/.config/fish"
	cat >"$home/.bashrc" <<'RC'
source /usr/share/bash-completion/bash_completion
PS1='$ '
RC
	cat >"$home/.zshrc" <<'RC'
autoload -Uz compinit && compinit -u
PROMPT='$ '
RC
	cat >"$home/.config/fish/config.fish" <<'RC'
set -g fish_greeting ''
function fish_prompt; echo -n '$ '; end
RC
}

# completes prints what a new shell under home shows for a line whose values
# only bb can supply.
completes() {
	local shell=$1 home=$2 session="setup-$1"
	case $shell in
	bash) tmux new-session -d -s "$session" -x 120 -y 30 -c "$work" env HOME="$home" bash --rcfile "$home/.bashrc" -i ;;
	zsh) tmux new-session -d -s "$session" -x 120 -y 30 -c "$work" env HOME="$home" zsh -i ;;
	fish) tmux new-session -d -s "$session" -x 120 -y 30 -c "$work" env HOME="$home" fish -i ;;
	powershell)
		# With the profiles, which is the point; TabExpansion2 is what the
		# line editor calls on Tab.
		(cd "$work" && HOME="$home" pwsh -NoLogo -Command \
			'$line = "bb pr list --state "; (TabExpansion2 -inputScript $line -cursorColumn $line.Length).CompletionMatches.CompletionText -join " "' 2>&1)
		return
		;;
	esac
	sleep 3
	press "$session" 'bb pr list --state '
	tmux kill-session -t "$session"
}

setup() {
	local shell=$1 home=$2
	shift 2
	if ! output="$(HOME="$home" bb completion "$@" --shell "$shell" 2>&1)"; then
		report "$shell" "bb completion $*" "it failed" "$output"
	fi
}

for shell in bash zsh fish powershell; do
	home="$work/setup-$shell"
	plain_home "$home"

	screen="$(completes "$shell" "$home")"
	expect_absent "$shell" "before any setup" "$screen" "closed"

	setup "$shell" "$home" install
	screen="$(completes "$shell" "$home")"
	expect_in "$shell" "set up for the user" "$screen" open closed all

	setup "$shell" "$home" remove --yes
	screen="$(completes "$shell" "$home")"
	expect_absent "$shell" "removed for the user" "$screen" "closed"

	setup "$shell" "$home" install --all-users
	screen="$(completes "$shell" "$home")"
	expect_in "$shell" "set up for every user" "$screen" open closed all

	setup "$shell" "$home" remove --all-users --yes
	screen="$(completes "$shell" "$home")"
	expect_absent "$shell" "removed for every user" "$screen" "closed"
done

if [ "$failures" -gt 0 ]; then
	printf '\n%d shell completion check(s) failed.\n' "$failures"
	exit 1
fi

printf 'bash, zsh, fish and pwsh all behave.\n'
