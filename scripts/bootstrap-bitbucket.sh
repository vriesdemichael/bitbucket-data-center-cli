#!/usr/bin/env bash
# Bootstrap a fresh Bitbucket Data Center instance for CI.
#
# The Bitbucket Docker image does NOT auto-apply the license key. Depending on
# the product version, licensing either happens during the setup wizard or via
# the REST API after startup. This script:
#   1. Polls /status until FIRST_RUN or RUNNING.
#   2. On FIRST_RUN: walks the setup wizard, handling the licensing/settings
#      step when present and the admin-user step.
#   3. Polls /status until RUNNING.
#   4. If BITBUCKET_LICENSE_KEY is set and licensing was not completed during
#      the setup wizard, applies it via the REST API
#      (/rest/api/latest/admin/license).
#
# Usage:
#   BITBUCKET_LICENSE_KEY=<key> bash scripts/bootstrap-bitbucket.sh [base_url] [username] [password]
#
# Arguments (all optional, with defaults):
#   base_url  - Bitbucket base URL (default: http://localhost:7990)
#   username  - Admin username to create  (default: admin)
#   password  - Admin password to create  (default: admin)

set -euo pipefail

BASE_URL="${1:-http://localhost:7990}"
ADMIN_USERNAME="${2:-admin}"
ADMIN_PASSWORD="${3:-admin}"
ADMIN_EMAIL="admin@example.com"
ADMIN_DISPLAY_NAME="Admin"
COOKIE_JAR="$(mktemp ./bb_bootstrap_cookies.XXXXXX 2>/dev/null || mktemp /tmp/bb_bootstrap_cookies.XXXXXX)"
RESPONSE_BODY="$(mktemp ./bb_bootstrap_response.XXXXXX 2>/dev/null || mktemp /tmp/bb_bootstrap_response.XXXXXX)"
trap 'rm -f "$COOKIE_JAR" "$RESPONSE_BODY"' EXIT

MAX_WAIT_SECONDS=300
POLL_INTERVAL=5

log() { echo "[bootstrap-bitbucket] $*" >&2; }

extract_html_input_value() {
  python3 -c "
import re, sys
html = sys.argv[1]
name = re.escape(sys.argv[2])
patterns = [
    rf\"xsrfTokenValue['\\\"]?\s*:\s*['\\\"]?([a-f0-9]+)\",
    rf\"name=['\\\"]{name}['\\\"][^>]*value=['\\\"]([^'\\\"]*)\",
    rf\"value=['\\\"]([^'\\\"]*)['\\\"][^>]*name=['\\\"]{name}['\\\"]\",
]
for pat in patterns:
    match = re.search(pat, html)
    if match:
        print(match.group(1))
        sys.exit(0)
sys.exit(1)
" "$1" "$2"
}

extract_setup_step() {
  extract_html_input_value "$1" "step"
}

submit_setup_settings() {
  local setup_html="$1"
  local atl_token application_title setup_base_url http_code response_html response_step

  if [ -z "${BITBUCKET_LICENSE_KEY:-}" ]; then
    log "Setup requires a license key before admin-user creation, but BITBUCKET_LICENSE_KEY is not set."
    exit 1
  fi

  atl_token=$(extract_html_input_value "$setup_html" "atl_token")
  application_title=$(extract_html_input_value "$setup_html" "applicationTitle" 2>/dev/null || true)
  setup_base_url=$(extract_html_input_value "$setup_html" "baseUrl" 2>/dev/null || true)
  : "${application_title:=Bitbucket}"
  : "${setup_base_url:=${BASE_URL}}"

  log "Submitting licensing and settings step..."
  http_code=$(
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
      -o "$RESPONSE_BODY" -w "%{http_code}" \
      -X POST "${BASE_URL}/setup" \
      --data-urlencode "step=settings" \
      --data-urlencode "applicationTitle=${application_title}" \
      --data-urlencode "baseUrl=${setup_base_url}" \
      --data-urlencode "license-type=true" \
      --data-urlencode "license=${BITBUCKET_LICENSE_KEY}" \
      --data-urlencode "licenseDisplay=${BITBUCKET_LICENSE_KEY}" \
      --data-urlencode "atl_token=${atl_token}"
  )
  log "Settings POST HTTP status: ${http_code}"
  if [[ ! "$http_code" =~ ^(2[0-9]{2}|302)$ ]]; then
    log "Unexpected HTTP status ${http_code}. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi

  response_html=$(<"$RESPONSE_BODY")
  response_step=$(extract_setup_step "$response_html" 2>/dev/null || true)
  if [ "$http_code" = "200" ] && [ "$response_step" = "settings" ]; then
    log "Setup settings submission did not advance. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi
}

submit_setup_user() {
  local setup_html="$1"
  local atl_token http_code response_html response_step

  atl_token=$(extract_html_input_value "$setup_html" "atl_token")

  log "Creating admin user '${ADMIN_USERNAME}'..."
  http_code=$(
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
      -o "$RESPONSE_BODY" -w "%{http_code}" \
      -X POST "${BASE_URL}/setup" \
      --data-urlencode "step=user" \
      --data-urlencode "username=${ADMIN_USERNAME}" \
      --data-urlencode "fullname=${ADMIN_DISPLAY_NAME}" \
      --data-urlencode "email=${ADMIN_EMAIL}" \
      --data-urlencode "password=${ADMIN_PASSWORD}" \
      --data-urlencode "confirmPassword=${ADMIN_PASSWORD}" \
      --data-urlencode "skipJira=" \
      --data-urlencode "atl_token=${atl_token}"
  )
  log "Setup POST HTTP status: ${http_code}"
  if [[ ! "$http_code" =~ ^(2[0-9]{2}|302)$ ]]; then
    log "Unexpected HTTP status ${http_code}. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi

  response_html=$(<"$RESPONSE_BODY")
  response_step=$(extract_setup_step "$response_html" 2>/dev/null || true)
  if [ "$http_code" = "200" ] && [ "$response_step" = "user" ]; then
    log "Setup user submission did not advance. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi
}

apply_license_via_rest() {
  local license_response

  if [ -z "${BITBUCKET_LICENSE_KEY:-}" ]; then
    log "BITBUCKET_LICENSE_KEY not set; skipping license application."
    return 0
  fi

  log "Applying license key via REST API..."
  license_response=$(curl -sf -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
    -X POST "${BASE_URL}/rest/api/latest/admin/license" \
    -H 'Content-Type: application/json' \
    -d "{\"license\": \"${BITBUCKET_LICENSE_KEY}\"}" 2>&1) && {
    log "License applied."
    return 0
  }

  license_response=$(curl -sf -u "${ADMIN_USERNAME}:${ADMIN_PASSWORD}" \
    -X POST "${BASE_URL}/rest/api/latest/admin/license" \
    -H 'Content-Type: application/json' \
    -d "{\"license\": \"${BITBUCKET_LICENSE_KEY}\"}" 2>&1) && {
    log "License applied."
    return 0
  }

  log "Failed to apply license. Response: ${license_response}"
  exit 1
}

authenticate_admin_session() {
  local http_code

  log "Authenticating admin session via TSV login API..."
  http_code=$(
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
      -o "$RESPONSE_BODY" -w "%{http_code}" \
      -X POST "${BASE_URL}/rest/tsv/latest/authenticate" \
      -H 'Accept: application/json' \
      -H 'Content-Type: application/json' \
      -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\",\"targetUrl\":\"/\",\"rememberMe\":false}"
  )
  if [[ ! "$http_code" =~ ^20[0-9]$ ]]; then
    log "Failed to authenticate admin session. HTTP status: ${http_code}. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi
}

authenticate_websudo() {
  local http_code atl_token websudo_html

  log "Authenticating WebSudo session..."
  websudo_html=$(curl -sf -b "$COOKIE_JAR" -c "$COOKIE_JAR" "${BASE_URL}/websudo" 2>/dev/null || true)
  atl_token=$(extract_html_input_value "$websudo_html" "atl_token" 2>/dev/null || true)

  http_code=$(
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
      -o "$RESPONSE_BODY" -w "%{http_code}" \
      -X POST "${BASE_URL}/websudo" \
      --data-urlencode "j_username=${ADMIN_USERNAME}" \
      --data-urlencode "j_password=${ADMIN_PASSWORD}" \
      ${atl_token:+--data-urlencode "atl_token=${atl_token}"}
  )
  log "WebSudo POST HTTP status: ${http_code}"
}

enable_basic_auth() {
  local http_code

  authenticate_admin_session
  authenticate_websudo

  log "Enabling basic authentication for bootstrap-driven admin API calls..."
  http_code=$(
    curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
      -o "$RESPONSE_BODY" -w "%{http_code}" \
      -X PUT "${BASE_URL}/rest/basicauth/latest/config" \
      -H 'Accept: application/json' \
      -H 'Content-Type: application/json' \
      -d '{"block-requests":false,"allowed-paths":[],"allowed-users":[],"show-warning-message":false}'
  )
  if [ "$http_code" != "204" ]; then
    log "Failed to enable basic authentication. HTTP status: ${http_code}. Response body:"
    cat "$RESPONSE_BODY" >&2
    exit 1
  fi
}

# Print the Bitbucket server state (STARTING, FIRST_RUN, RUNNING, or empty on
# error) by querying /status.
query_state() {
  local response
  response=$(curl -sf "${BASE_URL}/status" 2>/dev/null) || { echo ""; return 0; }
  python3 -c "
import sys, json
try:
    print(json.loads(sys.argv[1]).get('state', ''))
except Exception:
    print('')
" "$response"
}

# Poll /status until one of the expected states is reached.
# Prints the matched state to stdout.  Exits non-zero on timeout.
wait_for_states() {
  local elapsed=0
  while true; do
    local state
    state=$(query_state)
    log "State: ${state:-UNAVAILABLE} (${elapsed}s elapsed)"
    for target in "$@"; do
      if [ "$state" = "$target" ]; then
        echo "$state"
        return 0
      fi
    done
    if [ "$elapsed" -ge "$MAX_WAIT_SECONDS" ]; then
      log "Timeout after ${MAX_WAIT_SECONDS}s waiting for state(s): $*"
      return 1
    fi
    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
  done
}

log "Waiting for Bitbucket to be ready (${BASE_URL})..."
current_state=$(wait_for_states "FIRST_RUN" "RUNNING")
license_configured_during_setup=false

if [ "$current_state" = "FIRST_RUN" ]; then
  while true; do
    current_state=$(query_state)
    if [ "$current_state" = "RUNNING" ]; then
      break
    fi
    if [ "$current_state" != "FIRST_RUN" ]; then
      current_state=$(wait_for_states "FIRST_RUN" "RUNNING")
      if [ "$current_state" = "RUNNING" ]; then
        break
      fi
    fi

    log "Fetching setup page..."
    setup_html=$(curl -sf -b "$COOKIE_JAR" -c "$COOKIE_JAR" "${BASE_URL}/setup" 2>/dev/null)
    setup_step=$(extract_setup_step "$setup_html" 2>/dev/null || true)

    case "$setup_step" in
      settings)
        submit_setup_settings "$setup_html"
        license_configured_during_setup=true
        ;;
      user)
        submit_setup_user "$setup_html"
        ;;
      "")
        log "Could not determine setup step from setup page. Response body:"
        printf '%s\n' "$setup_html" >&2
        exit 1
        ;;
      *)
        log "Unsupported setup step '${setup_step}'. Response body:"
        printf '%s\n' "$setup_html" >&2
        exit 1
        ;;
    esac
  done

  log "Waiting for Bitbucket to reach RUNNING state..."
  wait_for_states "RUNNING"
fi

if [ "$license_configured_during_setup" != "true" ]; then
  apply_license_via_rest
fi

enable_basic_auth

log "Bitbucket is RUNNING at ${BASE_URL} (admin user: ${ADMIN_USERNAME})"
