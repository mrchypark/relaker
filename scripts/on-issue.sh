#!/bin/sh
set -eu

log="${TMPDIR:-/tmp}/relaker-issues.log"
printf '%s repo=%s action=%s delivery=%s payload=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${RELAKER_REPO:-}" \
  "${RELAKER_ACTION:-}" \
  "${RELAKER_ID:-}" \
  "${EVENT_PAYLOAD_FILE:-}" >> "$log"

if command -v osascript >/dev/null 2>&1; then
  osascript <<'OSA'
set repoName to system attribute "RELAKER_REPO"
if repoName is "" then set repoName to "GitHub"
display notification repoName with title "relaker issue opened"
OSA
fi
