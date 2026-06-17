#!/bin/sh
set -eu

log="${TMPDIR:-/tmp}/relaker-slack.log"
printf '%s event=%s channel=%s user=%s payload=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${RELAKER_EVENT:-}" \
  "${RELAKER_SLACK_CHANNEL:-}" \
  "${RELAKER_SLACK_USER:-}" \
  "${EVENT_PAYLOAD_FILE:-}" >> "$log"
