#!/bin/sh
set -eu

echo "relaker github event: ${RELAKER_REPO} ${RELAKER_EVENT} ${RELAKER_ACTION} base=${RELAKER_BASE_REF}"
echo "payload: ${EVENT_PAYLOAD_FILE}"
