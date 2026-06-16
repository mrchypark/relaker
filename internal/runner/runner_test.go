package runner_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrchypark/relaker/internal/rules"
	"github.com/mrchypark/relaker/internal/runner"
)

func TestRunnerExecutesOnlyAllowlistedLocalScriptWithPayloadFile(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "record.sh")
	outPath := filepath.Join(dir, "out.txt")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
test -f "$EVENT_PAYLOAD_FILE"
printf '%s|%s|%s|%s\n' "$RELAKER_SOURCE" "$RELAKER_EVENT" "$RELAKER_ACTION" "$(cat "$EVENT_PAYLOAD_FILE")" > "$OUT_PATH"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := runner.New(dir, []string{"scripts/record.sh"})
	if err != nil {
		t.Fatal(err)
	}

	err = r.Run(context.Background(), rules.Rule{Run: "scripts/record.sh"}, rules.Event{
		Source:  "github",
		Event:   "pull_request",
		Action:  "opened",
		Payload: []byte(`{"repository":{"full_name":"my-org/my-repo"}}`),
	}, []string{"OUT_PATH=" + outPath})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `github|pull_request|opened|{"repository"`) {
		t.Fatalf("unexpected output %q", string(got))
	}
}

func TestRunnerRejectsUnallowlistedScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "allowed.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "other.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := runner.New(dir, []string{"scripts/allowed.sh"})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Run(context.Background(), rules.Rule{Run: "scripts/other.sh"}, rules.Event{}, nil); err == nil {
		t.Fatal("Run returned nil error for unallowlisted script")
	}
}

func TestRunnerRejectsAbsoluteAllowlistPath(t *testing.T) {
	dir := t.TempDir()
	_, err := runner.New(dir, []string{filepath.Join(dir, "scripts", "record.sh")})
	if err == nil {
		t.Fatal("New returned nil error for absolute allowlist path")
	}
}

func TestRunnerRejectsSymlinkScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "escape.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "scripts", "escape.sh")); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/escape.sh"})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Run(context.Background(), rules.Rule{Run: "scripts/escape.sh"}, rules.Event{}, nil); err == nil {
		t.Fatal("Run returned nil error for symlink script")
	}
}

func TestRunnerRejectsSymlinkParentDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "escape.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "scripts")); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/escape.sh"})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Run(context.Background(), rules.Rule{Run: "scripts/escape.sh"}, rules.Event{}, nil); err == nil {
		t.Fatal("Run returned nil error for script through symlinked parent")
	}
}

func TestRunnerIncludesTruncatedFailedScriptStderr(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "fail.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s\n' "stdout-hidden"
printf '%s\n' "stderr-prefix" >&2
printf '%s\n' "$SECRET_TEXT" >&2
exit 9
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/fail.sh"})
	if err != nil {
		t.Fatal(err)
	}

	err = r.Run(context.Background(), rules.Rule{Run: "scripts/fail.sh"}, rules.Event{}, []string{"SECRET_TEXT=" + strings.Repeat("secret", 300)})
	if err == nil {
		t.Fatal("Run returned nil error for failing script")
	}
	if !strings.Contains(err.Error(), "stderr-prefix") {
		t.Fatalf("error did not include stderr snippet: %q", err.Error())
	}
	if strings.Contains(err.Error(), strings.Repeat("secret", 300)) {
		t.Fatalf("error leaked full stderr: %q", err.Error())
	}
	if strings.Contains(err.Error(), "stdout-hidden") {
		t.Fatalf("error included stdout: %q", err.Error())
	}
}

func TestRunnerCancelsScriptWithContext(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "sleep.sh")
	outPath := filepath.Join(dir, "out.txt")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
sleep 5
echo done > "$OUT_PATH"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/sleep.sh"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx, rules.Rule{Run: "scripts/sleep.sh"}, rules.Event{}, []string{"OUT_PATH=" + outPath}); err == nil {
		t.Fatal("Run returned nil error for canceled context")
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("output exists after canceled run, err=%v", err)
	}
}

func TestRunnerCancelsScriptProcessGroup(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "scripts", "spawn-child.sh")
	markerPath := filepath.Join(dir, "started.txt")
	childPath := filepath.Join(dir, "child.txt")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
(sleep 0.2; echo child > "$CHILD_PATH") &
echo started > "$MARKER_PATH"
sleep 5
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/spawn-child.sh"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, rules.Rule{Run: "scripts/spawn-child.sh"}, rules.Event{}, []string{
			"MARKER_PATH=" + markerPath,
			"CHILD_PATH=" + childPath,
		})
	}()
	waitForFile(t, markerPath)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil error for canceled script")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for canceled script")
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(childPath); !os.IsNotExist(err) {
		t.Fatalf("child output exists after process group cancel, err=%v", err)
	}
}

func TestRunnerDoesNotExposeGatewaySecretsFromProcessEnv(t *testing.T) {
	t.Setenv("RELAKER_GITHUB_SECRET", "github-secret")
	t.Setenv("SLACK_APP_TOKEN", "xapp-secret")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-secret")

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	scriptPath := filepath.Join(dir, "scripts", "env-check.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
if [ "${RELAKER_GITHUB_SECRET+x}" = "x" ] || [ "${SLACK_APP_TOKEN+x}" = "x" ] || [ "${SLACK_BOT_TOKEN+x}" = "x" ]; then
  echo "secret leaked" > "` + outPath + `"
  exit 9
fi
printf '%s|%s\n' "$RELAKER_SOURCE" "$RELAKER_EVENT" > "` + outPath + `"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/env-check.sh"})
	if err != nil {
		t.Fatal(err)
	}

	err = r.Run(context.Background(), rules.Rule{Run: "scripts/env-check.sh"}, rules.Event{
		Source: "slack",
		Event:  "app_mention",
	}, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "slack|app_mention" {
		t.Fatalf("output = %q", string(got))
	}
}

func TestRunnerPassesWindowsSystemEnvironment(t *testing.T) {
	t.Setenv("SystemRoot", "/windows")
	t.Setenv("SystemDrive", "C:")

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	scriptPath := filepath.Join(dir, "scripts", "windows-env.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s|%s\n' "$SystemRoot" "$SystemDrive" > "` + outPath + `"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, []string{"scripts/windows-env.sh"})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Run(context.Background(), rules.Rule{Run: "scripts/windows-env.sh"}, rules.Event{}, nil); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "/windows|C:" {
		t.Fatalf("output = %q", string(got))
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
