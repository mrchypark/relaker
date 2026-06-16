#!/bin/sh
set -eu

echo "relaker slack event: ${RELAKER_EVENT} channel=${RELAKER_SLACK_CHANNEL} user=${RELAKER_SLACK_USER}"
echo "payload: ${EVENT_PAYLOAD_FILE}"
