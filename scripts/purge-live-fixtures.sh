#!/usr/bin/env bash
# Remove the projects the live suite left on a local Bitbucket.
#
# Until this was written the harness deleted a seeded project with one call and
# discarded the response. Bitbucket answers 409 while the project still holds
# repositories, so every run since had been leaving its fixtures behind: 1000
# projects and 1032 repositories on the local instance when it was noticed. The
# harness deletes repositories first now; this clears what accumulated before.
#
# Only projects whose key starts with LT, which is the prefix every seeded
# project uses and nothing else does. Personal projects (~USER) and anything
# you created yourself are left alone.
#
# Usage: scripts/purge-live-fixtures.sh [url] [user] [password]
set -euo pipefail

url="${1:-${BITBUCKET_URL:-http://localhost:7990}}"
user="${2:-${BITBUCKET_USERNAME:-${ADMIN_USER:-admin}}}"
password="${3:-${BITBUCKET_PASSWORD:-${ADMIN_PASSWORD:-admin}}}"

api() { curl -sS -u "$user:$password" "$@"; }

keys=$(api "$url/rest/api/latest/projects?limit=1000" \
  | grep -o '"key":"LT[^"]*"' | sed 's/"key":"//;s/"$//' | sort -u)

if [ -z "$keys" ]; then
  echo "No LT* projects to remove."
  exit 0
fi

total=$(printf '%s\n' "$keys" | grep -c .)
echo "Removing $total seeded project(s) and their repositories from $url"

removed=0
for key in $keys; do
  slugs=$(api "$url/rest/api/latest/projects/$key/repos?limit=100" \
    | grep -o '"slug":"[^"]*"' | sed 's/"slug":"//;s/"$//')
  for slug in $slugs; do
    api -o /dev/null -X DELETE "$url/rest/api/latest/projects/$key/repos/$slug" || true
  done

  # Repository deletion is asynchronous, so the project delete is retried
  # rather than attempted once.
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if api -o /dev/null -f -X DELETE "$url/rest/api/latest/projects/$key" 2>/dev/null; then
      removed=$((removed + 1))
      break
    fi
    sleep 1
  done
done

echo "Removed $removed of $total."
