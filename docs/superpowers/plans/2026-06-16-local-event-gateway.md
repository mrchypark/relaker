# Local Event Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a small local Go daemon that accepts selected GitHub and Slack events, filters them with local YAML rules, dedupes deliveries, and runs only allowlisted local scripts.

**Architecture:** Provider receivers normalize GitHub webhook and Slack Socket Mode events into a shared event type. A gateway pipeline verifies, dedupes, matches rules, logs match/skip reasons, and invokes a safe runner that passes event data by environment variables and a temporary JSON payload file.

**Tech Stack:** Go, stdlib HTTP/log/exec/crypto packages, minimal YAML parsing dependency, optional Slack Socket Mode dependency, POSIX `sh` example scripts.

---

## File Structure

- `go.mod`: module definition and minimal dependencies.
- `cmd/relaker/main.go`: CLI flags, config loading, gateway wiring, HTTP server, optional Slack Socket Mode start.
- `internal/config`: YAML config schema and validation.
- `internal/gateway`: shared event pipeline, async dispatch, dedupe checks, rule evaluation, logging.
- `internal/github`: `/github` HTTP handler, HMAC verification, GitHub payload normalization.
- `internal/slack`: Slack Socket Mode envelope normalization and ack-oriented processing surface.
- `internal/rules`: rule matching for GitHub and Slack event fields.
- `internal/dedupe`: in-memory MVP dedupe store.
- `internal/runner`: allowlisted local script validation and execution.
- `config/relaker.example.yaml`: safe sample rules.
- `scripts/*.sh`: example allowlisted scripts.
- `README.md`: setup, secrets, GitHub forwarding, Slack Socket Mode, and test commands.

## Tasks

### Task 1: Config And Rule Matching

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/rules/rules.go`
- Test: `internal/config/config_test.go`
- Test: `internal/rules/rules_test.go`

- [ ] Write failing tests for loading YAML rules and matching GitHub/Slack sample events.
- [ ] Run focused tests and confirm they fail because the packages are missing.
- [ ] Implement config structs, validation, regex compilation checks, and rule matching.
- [ ] Run focused tests and confirm they pass.

### Task 2: Dedupe And Runner

**Files:**
- Create: `internal/dedupe/dedupe.go`
- Create: `internal/runner/runner.go`
- Test: `internal/dedupe/dedupe_test.go`
- Test: `internal/runner/runner_test.go`

- [ ] Write failing tests for first-seen vs duplicate delivery IDs.
- [ ] Write failing tests proving only configured local scripts can run and payload is passed through `EVENT_PAYLOAD_FILE`.
- [ ] Implement in-memory dedupe and script execution without shell interpolation.
- [ ] Run focused tests and confirm they pass.

### Task 3: GitHub Receiver

**Files:**
- Create: `internal/github/handler.go`
- Test: `internal/github/handler_test.go`

- [ ] Write failing tests for valid signature, invalid signature, normalized pull request fields, and fast 2xx async dispatch.
- [ ] Implement HMAC SHA-256 verification and HTTP handler normalization.
- [ ] Run focused tests and confirm they pass.

### Task 4: Slack Receiver

**Files:**
- Create: `internal/slack/socket.go`
- Test: `internal/slack/socket_test.go`

- [ ] Write failing tests for Slack event_callback normalization and dedupe IDs from `event_id` or envelope ID.
- [ ] Implement Slack sample processing without requiring live tokens in tests.
- [ ] Add live Socket Mode wiring behind env vars for app-level and bot tokens.
- [ ] Run focused tests and confirm they pass.

### Task 5: Gateway Wiring And Docs

**Files:**
- Create: `internal/gateway/gateway.go`
- Test: `internal/gateway/gateway_test.go`
- Create: `config/relaker.example.yaml`
- Create: `scripts/on-pr.sh`
- Create: `scripts/deploy-staging.sh`
- Modify: `cmd/relaker/main.go`
- Modify: `README.md`

- [ ] Write failing end-to-end tests for match, skip, dedupe, and script execution using GitHub and Slack samples.
- [ ] Implement async gateway dispatch, structured logs, CLI flags, and server wiring.
- [ ] Document local usage with `gh webhook forward` and Slack Socket Mode env vars.
- [ ] Run `gofmt` and `go test ./...`.

## Completion Check

- [ ] `go test ./...` passes.
- [ ] GitHub test payload covers rule match, skip, HMAC verification, dedupe, and script execution.
- [ ] Slack sample event covers rule match, skip, dedupe, and script execution.
- [ ] No payload values are interpolated into shell command strings.
- [ ] Secrets are read from env/local config and are not committed.
