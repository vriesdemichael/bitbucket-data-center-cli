#!/usr/bin/env bash
# The local Bitbucket instance the live suite runs against: one per checkout.
#
# Git worktrees let several agents work on this repository at once, and they all
# used to share one container. A restart in one worktree ended every other
# worktree's live run, a fixture purge removed projects another run was still
# using, and whoever restarted reset the licence clock for everyone (#623). Each
# checkout now has its own instance: its own compose project, container, Maven
# cache volume and licence.
#
# The main checkout keeps the project name, the host ports 7990 and 7999, and
# the URL every document and .env file names. A linked worktree gets a project
# named after it and host ports Docker assigns, which cannot collide. Bitbucket
# builds its HTTP links from the host a request names, so a clone link from an
# instance on another port still points back at that instance. Its SSH links
# keep port 7999, and `bb repo clone` falls back to HTTPS when SSH fails.
#
# Docker assigns a new port each time a stopped container starts, so `up` writes
# the instance's URL to .tmp/bitbucket.env every time. The live suite, the
# bootstrap and the fixture purge read it from there.
#
# Usage: scripts/stack.sh up|bootstrap|down|restart|reset|status|logs|prune|purge-fixtures
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

readonly compose_file=docker/compose.yml
readonly state_file=.tmp/bitbucket.env
readonly worktree_label=dev.bb-cli.worktree
# Each instance is a Bitbucket JVM of about 6GB. Past this many, `up` refuses to
# start another rather than let the machine swap.
readonly max_instances="${BB_STACK_MAX:-4}"
# The live suite refuses to start against an instance with less than this left
# before it stops itself (licenceMinimumRemaining in
# tests/integration/live/licence_test.go), so `up` restarts one that close.
readonly minimum_remaining_seconds=600

worktree="$(git rev-parse --show-toplevel)"
readonly worktree

if [ "$(git rev-parse --path-format=absolute --git-dir)" = "$(git rev-parse --path-format=absolute --git-common-dir)" ]; then
  project=bitbucket-data-center-cli
  http_port="${BITBUCKET_HOST_PORT:-7990}"
  ssh_port="${BITBUCKET_SSH_HOST_PORT:-7999}"
else
  name="$(basename "$worktree" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n' '-' | cut -c1-30)"
  hash="$(printf '%s' "$worktree" | git hash-object --stdin | cut -c1-6)"
  project="bb-${name%-}-${hash}"
  # 0 asks Docker for a free port.
  http_port=0
  ssh_port=0
fi
readonly project http_port ssh_port

compose() {
  BITBUCKET_HOST_PORT="$http_port" \
    BITBUCKET_SSH_HOST_PORT="$ssh_port" \
    BB_STACK_WORKTREE="$worktree" \
    docker compose -p "$project" -f "$compose_file" "$@"
}

running_container() {
  compose ps -q --status running bitbucket 2>/dev/null || true
}

# remaining_seconds prints how long this checkout's instance has before it stops
# itself, and fails when it is not running.
remaining_seconds() {
  local container issued retire
  container="$(running_container)"
  [ -n "$container" ] || return 1
  issued="$(docker exec "$container" cat /tmp/licence-issued-at 2>/dev/null)" || return 1
  retire="$(docker exec "$container" printenv BB_LICENCE_RETIRE_SECONDS 2>/dev/null)" || return 1
  echo $(( issued + retire - $(date +%s) ))
}

# other_instances lists the running instances that belong to other checkouts:
# project, worktree and how long ago each started, one per line.
other_instances() {
  docker ps --filter "label=${worktree_label}" \
    --format '{{.Label "com.docker.compose.project"}}	{{.Label "dev.bb-cli.worktree"}}	{{.RunningFor}}' \
    | awk -F '\t' -v own="$project" '$1 != own'
}

write_state() {
  local container name port
  container="$(running_container)"
  name="$(docker inspect --format '{{.Name}}' "$container" | sed 's#^/##')"
  port="$(compose port bitbucket 7990 | head -n 1 | sed 's/.*://')"
  mkdir -p "$(dirname "$state_file")"
  {
    echo "# Written by scripts/stack.sh up: this checkout's own Bitbucket instance."
    echo "BITBUCKET_URL=http://localhost:${port}"
    echo "BB_STACK_CONTAINER=${name}"
  } > "$state_file"
}

prune() {
  local other path
  docker ps -a --filter "label=${worktree_label}" \
    --format '{{.Label "com.docker.compose.project"}}	{{.Label "dev.bb-cli.worktree"}}' \
    | sort -u \
    | while IFS="$(printf '\t')" read -r other path; do
        if [ -z "$other" ] || [ -z "$path" ] || [ "$other" = "$project" ]; then
          continue
        fi
        if [ ! -d "$path" ]; then
          echo "Removing ${other}: its worktree ${path} no longer exists."
          docker compose -p "$other" down --volumes > /dev/null
        fi
      done
}

# bootstrap enables basic authentication, which the live suite logs in with and
# Bitbucket 10 refuses until it is turned on, even once it reports RUNNING. The
# SDK provisions the instance again on every start, so this runs after every
# `up` rather than being a step to remember: without it every live test answers
# 403, which reads like a product bug rather than a missing setup step.
bootstrap() {
  if [ -f .env ]; then
    set -a
    # shellcheck disable=SC1091
    . ./.env
    set +a
  fi
  # After .env, so the URL is this checkout's instance whatever .env names.
  # shellcheck disable=SC1090
  . "./${state_file}"
  bash scripts/bootstrap-bitbucket.sh "$BITBUCKET_URL" "${ADMIN_USER:-admin}" "${ADMIN_PASSWORD:-admin}"
}

up() {
  local others count remaining
  prune

  if [ -z "$(running_container)" ]; then
    others="$(other_instances)"
    count="$(printf '%s' "$others" | grep -c . || true)"
    if [ "$count" -ge "$max_instances" ]; then
      echo "Local Bitbucket instances already running: ${count}, and this machine holds at most ${max_instances} (BB_STACK_MAX):" >&2
      printf '%s\n' "$others" | awk -F '\t' '{ printf "  %s  %s  (started %s)\n", $1, ($2 == "" ? "?" : $2), $3 }' >&2
      echo "" >&2
      echo "Stop one with 'task stack:down' in its worktree, or raise BB_STACK_MAX." >&2
      exit 1
    fi
  elif remaining="$(remaining_seconds)" && [ "$remaining" -lt "$minimum_remaining_seconds" ]; then
    echo "This instance stops itself in $(( remaining / 60 ))m, too soon for a live run; starting it again with a new licence."
    compose stop bitbucket > /dev/null
  fi

  # --build so a change to the harness reaches a checkout that already has the
  # image: compose builds only a missing one. With nothing changed it rebuilds
  # from cache in seconds and leaves a running container alone.
  if ! compose up -d --build --wait; then
    echo "" >&2
    echo "The stack did not come up healthy." >&2
    echo "" >&2
    echo "On a first start the ~800MB download can outlast the start period;" >&2
    echo "'task stack:logs' shows progress. Otherwise check 'task stack:status'," >&2
    echo "then:" >&2
    echo "" >&2
    echo "    task stack:restart" >&2
    exit 1
  fi

  write_state
  bootstrap
}

down() {
  compose down
  rm -f "$state_file"
}

status() {
  local remaining
  compose ps
  if remaining="$(remaining_seconds)"; then
    echo "SDK licence: the instance stops itself in $(( remaining / 60 ))m; 'task stack:up' then starts it with a new one"
  else
    echo "SDK licence: not running (the instance stops itself when its licence ages out; 'task stack:up' starts it)"
  fi
  if [ -f "$state_file" ]; then
    # shellcheck disable=SC1090
    echo "URL: $(. "./${state_file}" && echo "$BITBUCKET_URL")"
  fi
  echo ""
  echo "Local Bitbucket instances on this machine:"
  docker ps -a --filter "label=${worktree_label}" \
    --format '  {{.Label "com.docker.compose.project"}}  {{.Status}}  {{.Label "dev.bb-cli.worktree"}}'
}

case "${1:-}" in
  up) up ;;
  bootstrap) bootstrap ;;
  down) down ;;
  restart) down && up ;;
  reset)
    compose down --volumes --remove-orphans
    rm -f "$state_file"
    up
    ;;
  status) status ;;
  logs) compose logs -f bitbucket ;;
  prune) prune ;;
  purge-fixtures)
    # shellcheck disable=SC1090
    . "./${state_file}"
    bash scripts/purge-live-fixtures.sh "$BITBUCKET_URL"
    ;;
  *)
    echo "usage: scripts/stack.sh up|bootstrap|down|restart|reset|status|logs|prune|purge-fixtures" >&2
    exit 2
    ;;
esac
