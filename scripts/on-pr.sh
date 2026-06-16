#!/bin/sh
set -eu

log="${TMPDIR:-/tmp}/relaker-prs.log"
printf '%s repo=%s event=%s action=%s base=%s payload=%s\n' \
  "$(date -u +%FT%TZ)" \
  "${RELAKER_REPO:-}" \
  "${RELAKER_EVENT:-}" \
  "${RELAKER_ACTION:-}" \
  "${RELAKER_BASE_REF:-}" \
  "${EVENT_PAYLOAD_FILE:-}" >> "$log"
