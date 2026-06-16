package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mrchypark/relaker/internal/config"
)

func TestLoadYAMLRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
addr: "127.0.0.1:9090"
github_secret: "dev-secret"
rules:
  - source: github
    event: pull_request
    actions: [opened, synchronize]
    repo: my-org/my-repo
    base_ref: main
    run: scripts/on-pr.sh
  - source: slack
    event: app_mention
    channel: C0123456789
    text_regex: "^deploy staging"
    run: scripts/deploy-staging.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.GitHubSecret != "dev-secret" {
		t.Fatalf("GitHubSecret = %q", cfg.GitHubSecret)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("len(Rules) = %d", len(cfg.Rules))
	}
	if cfg.Rules[0].Run != "scripts/on-pr.sh" {
		t.Fatalf("first run = %q", cfg.Rules[0].Run)
	}
	if cfg.Rules[1].TextRegex != "^deploy staging" {
		t.Fatalf("second text_regex = %q", cfg.Rules[1].TextRegex)
	}
}

func TestLoadReceiverAndWorkspaceArrays(t *testing.T) {
	t.Setenv("RELAKER_GITHUB_WORK_SECRET", "github-secret")
	t.Setenv("SLACK_WORK_APP_TOKEN", "xapp-work")
	t.Setenv("SLACK_WORK_BOT_TOKEN", "xoxb-work")
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
      secret_env: RELAKER_GITHUB_WORK_SECRET
slack:
  workspaces:
    - name: work
      app_token_env: SLACK_WORK_APP_TOKEN
      bot_token_env: SLACK_WORK_BOT_TOKEN
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
  - source: slack
    workspace: work
    event: app_mention
    run: scripts/on-slack.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.GitHub.Receivers) != 1 || cfg.GitHub.Receivers[0].Path != "/github/work" {
		t.Fatalf("github receivers = %#v", cfg.GitHub.Receivers)
	}
	if got := cfg.GitHub.Receivers[0].Secret(); got != "github-secret" {
		t.Fatalf("github secret = %q", got)
	}
	if len(cfg.Slack.Workspaces) != 1 {
		t.Fatalf("slack workspaces = %#v", cfg.Slack.Workspaces)
	}
	appToken, botToken, ok := cfg.Slack.Workspaces[0].Tokens()
	if !ok || appToken != "xapp-work" || botToken != "xoxb-work" {
		t.Fatalf("tokens = %q %q %v", appToken, botToken, ok)
	}
	if cfg.Rules[0].Receiver != "work" || cfg.Rules[1].Workspace != "work" {
		t.Fatalf("rules = %#v", cfg.Rules)
	}
}

func TestLoadReceiverAllowsExplicitUnsignedDevMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
      allow_unsigned: true
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.GitHub.Receivers[0].AllowUnsigned {
		t.Fatalf("AllowUnsigned = false")
	}
}

func TestLoadRejectsReceiverWithoutSecretOrUnsignedOptIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load returned nil error for receiver without secret or unsigned opt-in")
	}
}

func TestLoadAllowsReceiverSecretEnvMissingUntilStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
      secret_env: RELAKER_GITHUB_WORK_SECRET
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err != nil {
		t.Fatalf("Load returned error for missing receiver secret env: %v", err)
	}
}

func TestLoadRejectsDuplicateReceiverPath(t *testing.T) {
	t.Setenv("RELAKER_GITHUB_WORK_SECRET", "secret")
	t.Setenv("RELAKER_GITHUB_HOME_SECRET", "secret")
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
      secret_env: RELAKER_GITHUB_WORK_SECRET
    - name: home
      path: /github/work
      secret_env: RELAKER_GITHUB_HOME_SECRET
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load returned nil error for duplicate receiver path")
	}
}

func TestLoadRejectsDuplicateReceiverName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
github:
  receivers:
    - name: work
      path: /github/work
      allow_unsigned: true
    - name: work
      path: /github/home
      allow_unsigned: true
rules:
  - source: github
    receiver: work
    event: pull_request
    run: scripts/on-pr.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load returned nil error for duplicate receiver name")
	}
}

func TestLoadRejectsDuplicateSlackWorkspaceName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
slack:
  workspaces:
    - name: work
      app_token_env: SLACK_WORK_APP_TOKEN
      bot_token_env: SLACK_WORK_BOT_TOKEN
    - name: work
      app_token_env: SLACK_HOME_APP_TOKEN
      bot_token_env: SLACK_HOME_BOT_TOKEN
rules:
  - source: slack
    workspace: work
    event: app_mention
    run: scripts/on-slack.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load returned nil error for duplicate slack workspace name")
	}
}

func TestExampleConfigScopesSlackRuleToWorkspace(t *testing.T) {
	t.Setenv("RELAKER_GITHUB_WORK_SECRET", "github-secret")
	cfg, err := config.Load(filepath.Join("..", "..", "config", "relaker.example.yaml"))
	if err != nil {
		t.Fatalf("Load example returned error: %v", err)
	}
	if len(cfg.GitHub.Receivers) != 1 || cfg.GitHub.Receivers[0].Path != "/github/work" {
		t.Fatalf("example github receivers = %#v", cfg.GitHub.Receivers)
	}
	if got := cfg.GitHub.Receivers[0].Secret(); got != "github-secret" {
		t.Fatalf("example github receiver secret = %q", got)
	}
	for _, rule := range cfg.Rules {
		if rule.Source == "slack" && rule.Workspace == "work" {
			return
		}
	}
	t.Fatalf("example rules missing workspace-scoped slack rule: %#v", cfg.Rules)
}

func TestLoadRejectsInvalidTextRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relaker.yaml")
	err := os.WriteFile(path, []byte(`
rules:
  - source: slack
    event: app_mention
    text_regex: "["
    run: scripts/deploy-staging.sh
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(path); err == nil {
		t.Fatal("Load returned nil error for invalid regex")
	}
}
