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
# A release argument (an atlassian/bitbucket tag such as 9.2.1) acts on an
# instance of that release instead, next to the checkout's own: its own compose
# project, image tag, Docker-assigned ports and .tmp/bitbucket-<release>.env. It
# is how the live suite is run against the older releases bb supports.
#
# Usage: scripts/stack.sh up|bootstrap|down|restart|reset|status|logs|prune|purge-fixtures [release]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

readonly compose_file=docker/compose.yml
readonly worktree_label=dev.bb-cli.worktree
# Each instance is a Bitbucket JVM of about 6GB. Past this many, `up` refuses to
# start another rather than let the machine swap.
readonly max_instances="${BB_STACK_MAX:-4}"
# Ages from the licence's issue. The container stops itself at
# BB_LICENCE_RETIRE_SECONDS, 2h58m, two minutes before the licence runs out.
# `up` starts an instance at least 2h40m old again, so a run `task test:live`
# starts has at least eighteen minutes before that stop. The live suite refuses
# a run from 2h45m (licenceLatestStartAge in tests/integration/live/licence_test.go):
# the five minutes between are what `task test:live` spends building the suite.
readonly restart_from_age_seconds=9600

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

release="${2:-}"
if [ -n "$release" ]; then
  if ! printf '%s' "$release" | grep -qE '^[0-9]+\.[0-9]+(\.[0-9]+)?$'; then
    echo "Not a Bitbucket release: '${release}'. Give an atlassian/bitbucket image tag, such as 9.2.1." >&2
    exit 2
  fi
  project="${project}-${release//./-}"
  # Never the main checkout's 7990: its own instance may be using it.
  http_port=0
  ssh_port=0
  state_file=".tmp/bitbucket-${release}.env"
  harness_tag="$release"
  # Appended to the task commands this script suggests, so they name the same instance.
  task_release=" RELEASE=${release}"
else
  state_file=.tmp/bitbucket.env
  harness_tag=local
  task_release=""
fi
readonly project http_port ssh_port release state_file harness_tag task_release

compose() {
  BITBUCKET_HOST_PORT="$http_port" \
    BITBUCKET_SSH_HOST_PORT="$ssh_port" \
    BB_STACK_WORKTREE="$worktree" \
    BB_HARNESS_TAG="$harness_tag" \
    docker compose -p "$project" -f "$compose_file" "$@"
}

# build_release_image builds the harness for a release other than the one the
# Dockerfile pins, from the same file with only the FROM tag replaced. The JVM,
# git and product version all follow from the base image, as they do for the
# pinned release, and the pin stays the one Dependabot moves (ADR-042).
build_release_image() {
  local dockerfile
  dockerfile="$(sed "s#^FROM atlassian/bitbucket:.*#FROM atlassian/bitbucket:${release}#" docker/harness/Dockerfile)"
  if ! printf '%s\n' "$dockerfile" | grep -qx "FROM atlassian/bitbucket:${release}"; then
    echo "docker/harness/Dockerfile has no 'FROM atlassian/bitbucket:<tag>' line to replace." >&2
    return 1
  fi
  printf '%s\n' "$dockerfile" | docker build --tag "bitbucket-cli-harness:${release}" --file - docker/harness
}

running_container() {
  compose ps -q --status running bitbucket 2>/dev/null || true
}

# licence_issued_at prints when the instance in a container was issued its
# licence.
#
# MSYS_NO_PATHCONV, because Git Bash on Windows rewrites an argument starting
# with / into a Windows path before docker sees it. The read failed there, so
# `up` never started an aged instance again and `status` called it not running.
licence_issued_at() {
  MSYS_NO_PATHCONV=1 docker exec "$1" cat /tmp/licence-issued-at 2>/dev/null
}

# age_seconds prints how long ago this checkout's instance was issued its
# licence, and fails when it is not running.
age_seconds() {
  local container issued
  container="$(running_container)"
  [ -n "$container" ] || return 1
  issued="$(licence_issued_at "$container")" || return 1
  echo $(( $(date +%s) - issued ))
}

# remaining_seconds prints how long this checkout's instance has before it stops
# itself, and fails when it is not running.
remaining_seconds() {
  local container issued retire
  container="$(running_container)"
  [ -n "$container" ] || return 1
  issued="$(licence_issued_at "$container")" || return 1
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

# prune takes down the instances of other checkouts that have stopped: those
# whose worktree is gone with their Maven cache, the rest keeping it.
#
# A stopped instance still holds its compose network, and every network holds
# a subnet out of Docker's address pools. An instance stops itself when its
# licence ages out, so a worktree nobody runs the suite in again keeps one for
# good, and enough of them exhaust the pools: Docker then hands out subnets
# that collide with its own and with the LAN, and Docker Desktop stops
# answering (#652). Taking a stopped instance down keeps its cache volume, and
# its worktree's next `up` creates the rest again, with the new licence it
# would have been issued anyway.
#
# A running instance is never touched, even when its worktree looks gone. The
# path is the one recorded where the instance was started, and from another
# environment a worktree that exists can look missing: WSL records
# /mnt/c/..., which Git Bash cannot see, and Git Bash records C:/..., which
# WSL cannot, and a worktree on a drive that is not mounted is missing to
# both. Removing on that verdict alone could stop a live run in another
# session and delete its cache. An instance whose worktree really is gone
# stops itself within three hours, and the next prune removes it then. Nor
# is one that is created or restarting: that may be another worktree's `up`
# under way.
prune() {
  local other path state
  docker ps -a --filter "label=${worktree_label}" \
    --format '{{.Label "com.docker.compose.project"}}	{{.Label "dev.bb-cli.worktree"}}	{{.State}}' \
    | sort -u \
    | while IFS="$(printf '\t')" read -r other path state; do
        if [ -z "$other" ] || [ -z "$path" ] || [ "$other" = "$project" ]; then
          continue
        fi
        if [ "$state" != "exited" ] && [ "$state" != "dead" ]; then
          continue
        fi
        if [ ! -d "$path" ]; then
          echo "Removing ${other}: it is stopped, and its worktree ${path} no longer exists."
          docker compose -p "$other" down --volumes > /dev/null 2>&1
        else
          echo "Taking down ${other}: it is stopped, and its network is only in the way."
          docker compose -p "$other" down > /dev/null 2>&1
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
  local others count age build
  prune

  if [ -z "$(running_container)" ]; then
    others="$(other_instances)"
    count="$(printf '%s' "$others" | grep -c . || true)"
    if [ "$count" -ge "$max_instances" ]; then
      echo "Local Bitbucket instances already running: ${count}, and this machine holds at most ${max_instances} (BB_STACK_MAX):" >&2
      if [ -n "$others" ]; then
        printf '%s\n' "$others" | awk -F '\t' '{ printf "  %s  %s  (started %s)\n", $1, ($2 == "" ? "?" : $2), $3 }' >&2
      fi
      echo "" >&2
      echo "Stop one with 'task stack:down' in its worktree, or raise BB_STACK_MAX." >&2
      exit 1
    fi
  elif age="$(age_seconds)" && [ "$age" -ge "$restart_from_age_seconds" ]; then
    printf 'This instance is %dh%02dm into its licence, 2h40m or more; starting it again with a new licence.\n' \
      $(( age / 3600 )) $(( age % 3600 / 60 ))
    compose stop bitbucket > /dev/null
  fi

  # --build so a change to the harness reaches a checkout that already has the
  # image: compose builds only a missing one. With nothing changed it rebuilds
  # from cache in seconds and leaves a running container alone. A release's
  # image is built just before, from its own tag, and compose must not build
  # the pinned one over it.
  build=--build
  if [ -n "$release" ]; then
    build_release_image
    build=--no-build
  fi
  if ! compose up -d "$build" --wait; then
    echo "" >&2
    echo "The stack did not come up healthy." >&2
    echo "" >&2
    echo "On a first start the ~800MB download can outlast the start period;" >&2
    echo "'task stack:logs${task_release}' shows progress. Otherwise check 'task stack:status${task_release}'," >&2
    echo "then:" >&2
    echo "" >&2
    echo "    task stack:restart${task_release}" >&2
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
    echo "SDK licence: the instance stops itself in $(( remaining / 60 ))m; 'task stack:up${task_release}' then starts it with a new one"
  else
    echo "SDK licence: not running (the instance stops itself when its licence ages out; 'task stack:up${task_release}' starts it)"
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
    echo "usage: scripts/stack.sh up|bootstrap|down|restart|reset|status|logs|prune|purge-fixtures [release]" >&2
    exit 2
    ;;
esac
