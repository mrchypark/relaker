#!/bin/sh
set -eu

org="${1:-Conalog}"
url="${RELAKER_GITHUB_URL:-http://127.0.0.1:8080/github/conalog}"
limit="${RELAKER_GITHUB_REPO_LIMIT:-500}"
pids=""

cleanup() {
  for pid in $pids; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup INT TERM EXIT

repos=$(
  gh repo list "$org" --limit "$limit" \
    --json nameWithOwner,hasIssuesEnabled,viewerPermission \
    --jq '.[] | select(.hasIssuesEnabled and .viewerPermission != "READ") | .nameWithOwner'
)

if [ -z "$repos" ]; then
  echo "no writable issue-enabled repos found for $org" >&2
  exit 1
fi

for repo in $repos; do
  echo "forwarding $repo -> $url"
  gh webhook forward --repo="$repo" --events=issues --url="$url" &
  pids="$pids $!"
done

wait
