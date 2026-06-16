#!/bin/sh
set -eu

log="${TMPDIR:-/tmp}/relaker-issues.log"
printf '%s repo=%s action=%s delivery=%s payload=%s\n' \
  "$(date -u +%FT%TZ)" \
  "${RELAKER_REPO:-}" \
  "${RELAKER_ACTION:-}" \
  "${RELAKER_ID:-}" \
  "${EVENT_PAYLOAD_FILE:-}" >> "$log"

if command -v osascript >/dev/null 2>&1; then
  osascript -e 'display notification "GitHub issue opened" with title "relaker"'
fi
