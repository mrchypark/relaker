package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrchypark/relaker/internal/rules"
)

const scriptTimeout = 10 * time.Minute

type Runner struct {
	root    string
	allowed map[string]struct{}
}

func New(root string, allowlist []string) (*Runner, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	r := &Runner{root: absRoot, allowed: make(map[string]struct{}, len(allowlist))}
	for _, script := range allowlist {
		clean, err := cleanLocal(script)
		if err != nil {
			return nil, fmt.Errorf("allow %q: %w", script, err)
		}
		r.allowed[clean] = struct{}{}
	}
	return r, nil
}

func (r *Runner) Run(ctx context.Context, rule rules.Rule, event rules.Event, extraEnv []string) error {
	script, err := cleanLocal(rule.Run)
	if err != nil {
		return err
	}
	if _, ok := r.allowed[script]; !ok {
		return fmt.Errorf("script %q is not allowlisted", rule.Run)
	}
	path := filepath.Join(r.root, script)
	resolvedRoot, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve script: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return fmt.Errorf("script %q resolves outside root", rule.Run)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat script: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("script %q is a symlink", rule.Run)
	}
	if info.IsDir() {
		return fmt.Errorf("script %q is a directory", rule.Run)
	}

	payload := event.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	tmp, err := os.CreateTemp("", "relaker-event-*.json")
	if err != nil {
		return fmt.Errorf("create payload file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("write payload file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close payload file: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path)
	cmd.Dir = r.root
	cmd.Env = append(safeParentEnv(), envFor(event, tmpPath)...)
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %q: %w", rule.Run, err)
	}
	return nil
}

func safeParentEnv() []string {
	names := []string{"PATH", "HOME", "TMPDIR", "SHELL"}
	env := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func cleanLocal(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path must stay under the relaker root")
	}
	return clean, nil
}

func envFor(event rules.Event, payloadPath string) []string {
	return []string{
		"EVENT_PAYLOAD_FILE=" + payloadPath,
		"RELAKER_SOURCE=" + event.Source,
		"RELAKER_EVENT=" + event.Event,
		"RELAKER_ACTION=" + event.Action,
		"RELAKER_ID=" + event.ID,
		"RELAKER_ENVELOPE_ID=" + event.EnvelopeID,
		"RELAKER_REPO=" + event.Repo,
		"RELAKER_BASE_REF=" + event.BaseRef,
		"RELAKER_SLACK_CHANNEL=" + event.Channel,
		"RELAKER_SLACK_USER=" + event.User,
		"RELAKER_SLACK_TEXT=" + event.Text,
		"RELAKER_SLACK_REACTION=" + event.Reaction,
	}
}
