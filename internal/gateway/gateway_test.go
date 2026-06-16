package gateway_test

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/relaker/internal/dedupe"
	"github.com/mrchypark/relaker/internal/gateway"
	"github.com/mrchypark/relaker/internal/rules"
	"github.com/mrchypark/relaker/internal/runner"
)

func TestGatewayMatchesGitHubRuleDedupesAndExecutesScript(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "runs.txt")
	writeScript(t, dir, "scripts/on-pr.sh", out)
	rs := mustRuleSet(t, []rules.Rule{{
		Source:  "github",
		Event:   "pull_request",
		Actions: []string{"opened"},
		Repo:    "my-org/my-repo",
		BaseRef: "main",
		Run:     "scripts/on-pr.sh",
	}})
	r := mustRunner(t, dir, []string{"scripts/on-pr.sh"})
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)
	event := rules.Event{
		Source:  "github",
		Event:   "pull_request",
		Action:  "opened",
		ID:      "delivery-1",
		Repo:    "my-org/my-repo",
		BaseRef: "main",
		Payload: []byte(`{"action":"opened"}`),
	}

	if err := gw.Process(context.Background(), event, []string{"OUT_PATH=" + out}); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if err := gw.Process(context.Background(), event, []string{"OUT_PATH=" + out}); err != nil {
		t.Fatalf("duplicate Process returned error: %v", err)
	}

	got := readFile(t, out)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 1 {
		t.Fatalf("runs = %d, content = %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "github|pull_request|opened") {
		t.Fatalf("content = %q", got)
	}
}

func TestGatewayRetriesEventAfterScriptFailure(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "runs.txt")
	marker := filepath.Join(dir, "failed-once")
	scriptPath := filepath.Join(dir, "scripts", "flaky.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
if [ ! -f "$MARKER" ]; then
  touch "$MARKER"
  exit 7
fi
printf 'ok\n' >> "$OUT_PATH"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rs := mustRuleSet(t, []rules.Rule{{
		Source: "github",
		Event:  "pull_request",
		Run:    "scripts/flaky.sh",
	}})
	r := mustRunner(t, dir, []string{"scripts/flaky.sh"})
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)
	event := rules.Event{Source: "github", Event: "pull_request", ID: "delivery-1"}
	env := []string{"OUT_PATH=" + out, "MARKER=" + marker}

	if err := gw.Process(context.Background(), event, env); err == nil {
		t.Fatal("first Process returned nil error")
	}
	if err := gw.Process(context.Background(), event, env); err != nil {
		t.Fatalf("retry Process returned error: %v", err)
	}
	if got := readFile(t, out); strings.TrimSpace(got) != "ok" {
		t.Fatalf("output = %q", got)
	}
}

func TestGatewayDoesNotRetryCompletedSideEffectsAfterLaterRuleFailure(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "runs.txt")
	writeScript(t, dir, "scripts/ok.sh", out)
	failPath := filepath.Join(dir, "scripts", "fail.sh")
	if err := os.WriteFile(failPath, []byte("#!/bin/sh\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rs := mustRuleSet(t, []rules.Rule{
		{Source: "github", Event: "pull_request", Run: "scripts/ok.sh"},
		{Source: "github", Event: "pull_request", Run: "scripts/fail.sh"},
	})
	r := mustRunner(t, dir, []string{"scripts/ok.sh", "scripts/fail.sh"})
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)
	event := rules.Event{Source: "github", Event: "pull_request", ID: "delivery-1"}
	env := []string{"OUT_PATH=" + out}

	if err := gw.Process(context.Background(), event, env); err == nil {
		t.Fatal("first Process returned nil error")
	}
	if err := gw.Process(context.Background(), event, env); err != nil {
		t.Fatalf("duplicate Process returned error: %v", err)
	}
	got := readFile(t, out)
	if lines := strings.Split(strings.TrimSpace(got), "\n"); len(lines) != 1 {
		t.Fatalf("runs = %d, content = %q", len(lines), got)
	}
}

func TestGatewayDoesNotLogFailedScriptStderr(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "fail.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho stderr-secret >&2\nexit 9\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rs := mustRuleSet(t, []rules.Rule{{Source: "github", Event: "issues", Run: "scripts/fail.sh"}})
	r := mustRunner(t, dir, []string{"scripts/fail.sh"})
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)

	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)

	err := gw.Process(context.Background(), rules.Event{Source: "github", Event: "issues", ID: "delivery-1"}, nil)
	if err == nil {
		t.Fatal("Process returned nil error")
	}
	if !strings.Contains(err.Error(), "stderr-secret") {
		t.Fatalf("returned error should retain stderr for local debugging: %q", err.Error())
	}
	if strings.Contains(logs.String(), "stderr-secret") {
		t.Fatalf("gateway logs leaked script stderr: %q", logs.String())
	}
}

func TestGatewaySkipsSlackRuleMismatchAndExecutesMatch(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "runs.txt")
	writeScript(t, dir, "scripts/deploy-staging.sh", out)
	rs := mustRuleSet(t, []rules.Rule{{
		Source:    "slack",
		Event:     "app_mention",
		Channel:   "C0123456789",
		TextRegex: "^deploy staging",
		Run:       "scripts/deploy-staging.sh",
	}})
	r := mustRunner(t, dir, []string{"scripts/deploy-staging.sh"})
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)

	skip := rules.Event{
		Source:  "slack",
		Event:   "app_mention",
		ID:      "Ev-skip",
		Channel: "C0123456789",
		Text:    "deploy prod",
		Payload: []byte(`{"event":{"type":"app_mention"}}`),
	}
	if err := gw.Process(context.Background(), skip, []string{"OUT_PATH=" + out}); err != nil {
		t.Fatalf("skip Process returned error: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("output exists after skipped event, err=%v", err)
	}

	match := rules.Event{
		Source:     "slack",
		Event:      "app_mention",
		ID:         "Ev-match",
		EnvelopeID: "env-match",
		Channel:    "C0123456789",
		User:       "U123",
		Text:       "deploy staging",
		Payload:    []byte(`{"event":{"type":"app_mention"}}`),
	}
	if err := gw.Process(context.Background(), match, []string{"OUT_PATH=" + out}); err != nil {
		t.Fatalf("match Process returned error: %v", err)
	}
	got := readFile(t, out)
	if !strings.Contains(got, "slack|app_mention|") {
		t.Fatalf("content = %q", got)
	}
}

func writeScript(t *testing.T, root, rel, out string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s|%s|%s|%s\n' "$RELAKER_SOURCE" "$RELAKER_EVENT" "$RELAKER_ACTION" "$(cat "$EVENT_PAYLOAD_FILE")" >> "$OUT_PATH"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = out
}

func mustRuleSet(t *testing.T, ruleList []rules.Rule) *rules.Set {
	t.Helper()
	rs, err := rules.NewSet(ruleList)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func mustRunner(t *testing.T, root string, allowed []string) *runner.Runner {
	t.Helper()
	r, err := runner.New(root, allowed)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
