# relaker

`relaker` is a small local event gateway MVP. It receives GitHub webhooks and Slack Socket Mode envelopes, normalizes them into internal events, dedupes delivery IDs, filters with local YAML rules, and runs allowlisted local scripts.

## Quick Start

```sh
cp config/relaker.example.yaml config/relaker.yaml
go test ./...
go run ./cmd/relaker -config config/relaker.yaml
```

With no `github.receivers` configured, GitHub webhooks are accepted at:

```text
POST http://127.0.0.1:8080/github
```

For a signed GitHub webhook, set a secret in the environment:

```sh
export RELAKER_GITHUB_SECRET='local-dev-secret'
go run ./cmd/relaker -config config/relaker.yaml
```

For unsigned local-only GitHub testing, set `github_allow_unsigned: true`.

The handler verifies `X-Hub-Signature-256` when a secret is configured, then responds `202 Accepted` and processes the event asynchronously.

With GitHub CLI installed, forward repository webhooks to the local daemon with:

```sh
gh webhook forward --repo my-org/my-repo --events pull_request --url http://127.0.0.1:8080/github
```

For issue-created notifications, forward the `issues` event and add an `issues`
rule:

```sh
gh webhook forward --repo my-org/my-repo --events issues --url http://127.0.0.1:8080/github/work
```

To run multiple GitHub receivers, configure one path per receiver and put secrets in the named env vars:

```yaml
github:
  receivers:
    - name: work
      path: /github/work
      secret_env: RELAKER_GITHUB_WORK_SECRET
```

```sh
export RELAKER_GITHUB_WORK_SECRET='work-secret'
gh webhook forward --repo my-org/my-repo --events pull_request --url http://127.0.0.1:8080/github/work
```

For unsigned local-only receiver testing, use `allow_unsigned: true` on that receiver instead of `secret_env`.

## Rules

Rules are YAML:

```yaml
rules:
  - source: github
    receiver: work
    event: pull_request
    actions: [opened, synchronize, reopened]
    repo: my-org/my-repo
    base_ref: main
    labels_any: [ready-for-relaker]
    run: scripts/on-pr.sh
  - source: github
    receiver: work
    event: issues
    actions: [opened]
    repo: my-org/my-repo
    run: scripts/on-issue.sh
  - source: slack
    workspace: work
    event: app_mention
    channel: C0123456789
    text_regex: "^deploy staging"
    run: scripts/deploy-staging.sh
```

Only scripts named by configured rules are allowlisted. Script paths must be local relative paths under the relaker root. Scripts are executed directly with `exec`, not through a shell command string.

Minimum event data is passed by environment variables, including `RELAKER_SOURCE`, `RELAKER_EVENT`, `RELAKER_ACTION`, `RELAKER_REPO`, `RELAKER_BASE_REF`, `RELAKER_SLACK_CHANNEL`, `RELAKER_SLACK_USER`, `RELAKER_SLACK_TEXT`, `RELAKER_SLACK_REACTION`, and `EVENT_PAYLOAD_FILE`.

GitHub label filters use `labels_all` for labels that must all be present and `labels_any` for at least one acceptable label. Slack rules can also filter `event_id` and `envelope_id`.
GitHub rules can filter `receiver`; Slack rules can filter `workspace`. If omitted, the rule keeps the old behavior and matches any receiver or workspace for that source.

Scripts receive a minimal environment only: relaker event variables, `EVENT_PAYLOAD_FILE`, and safe parent variables `PATH`, `HOME`, `TMPDIR`, and `SHELL` when present. Gateway secrets such as `RELAKER_GITHUB_SECRET`, `SLACK_APP_TOKEN`, and `SLACK_BOT_TOKEN` are not forwarded to scripts.

## Slack Socket Mode

With no `slack.workspaces` configured, Slack Socket Mode starts automatically when both tokens are present:

```sh
export SLACK_APP_TOKEN='xapp-...'
export SLACK_BOT_TOKEN='xoxb-...'
go run ./cmd/relaker -config config/relaker.yaml
```

If either token is unset, relaker logs that Slack is disabled and continues serving GitHub. Tokens are read from environment variables only and are not part of the YAML config.

To run multiple Slack workspaces, configure the env var names per workspace:

```yaml
slack:
  workspaces:
    - name: work
      app_token_env: SLACK_WORK_APP_TOKEN
      bot_token_env: SLACK_WORK_BOT_TOKEN
```

```sh
export SLACK_WORK_APP_TOKEN='xapp-...'
export SLACK_WORK_BOT_TOKEN='xoxb-...'
go run ./cmd/relaker -config config/relaker.yaml
```

Socket Mode envelopes are acked before work is processed through the gateway.

## Slack Socket Mode Samples

The MVP includes a token-free Slack Socket Mode envelope processor for local tests and samples. It acks the envelope ID and converts `event_callback` payloads into relaker events.

Create a sample envelope:

```sh
cat > /tmp/slack-envelope.json <<'JSON'
{
  "envelope_id": "env-1",
  "type": "events_api",
  "payload": {
    "type": "event_callback",
    "event_id": "Ev123",
    "event": {
      "type": "app_mention",
      "channel": "C0123456789",
      "user": "U123",
      "text": "deploy staging"
    }
  }
}
JSON
go run ./cmd/relaker -config config/relaker.yaml -slack-envelope /tmp/slack-envelope.json -slack-workspace work
```

## Local Verification

```sh
gofmt -w cmd internal
go test ./...
```
