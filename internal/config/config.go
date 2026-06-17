package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/mrchypark/relaker/internal/rules"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Addr                string       `yaml:"addr"`
	GitHubSecret        string       `yaml:"github_secret"`
	GitHubAllowUnsigned bool         `yaml:"github_allow_unsigned"`
	GitHub              GitHubConfig `yaml:"github"`
	Slack               SlackConfig  `yaml:"slack"`
	Rules               []rules.Rule `yaml:"rules"`
}

type GitHubConfig struct {
	Receivers []GitHubReceiver `yaml:"receivers"`
}

type GitHubReceiver struct {
	Name          string `yaml:"name"`
	Path          string `yaml:"path"`
	SecretEnv     string `yaml:"secret_env"`
	AllowUnsigned bool   `yaml:"allow_unsigned"`
}

func (r GitHubReceiver) Secret() string {
	if r.SecretEnv == "" {
		return ""
	}
	return os.Getenv(r.SecretEnv)
}

type SlackConfig struct {
	Workspaces []SlackWorkspace `yaml:"workspaces"`
}

type SlackWorkspace struct {
	Name        string `yaml:"name"`
	AppTokenEnv string `yaml:"app_token_env"`
	BotTokenEnv string `yaml:"bot_token_env"`
}

func (w SlackWorkspace) Tokens() (string, string, bool) {
	if w.AppTokenEnv == "" || w.BotTokenEnv == "" {
		return "", "", false
	}
	appToken := os.Getenv(w.AppTokenEnv)
	botToken := os.Getenv(w.BotTokenEnv)
	return appToken, botToken, appToken != "" && botToken != ""
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	receiverPaths := make(map[string]int, len(cfg.GitHub.Receivers))
	receiverNames := make(map[string]int, len(cfg.GitHub.Receivers))
	for i, receiver := range cfg.GitHub.Receivers {
		if receiver.Name == "" {
			return nil, fmt.Errorf("github receiver %d: name is required", i)
		}
		if prev, ok := receiverNames[receiver.Name]; ok {
			return nil, fmt.Errorf("github receiver %d: name duplicates receiver %d", i, prev)
		}
		receiverNames[receiver.Name] = i
		if receiver.Path == "" {
			return nil, fmt.Errorf("github receiver %d: path is required", i)
		}
		if !strings.HasPrefix(receiver.Path, "/") {
			return nil, fmt.Errorf("github receiver %d: path must start with /", i)
		}
		if prev, ok := receiverPaths[receiver.Path]; ok {
			return nil, fmt.Errorf("github receiver %d: path duplicates receiver %d", i, prev)
		}
		receiverPaths[receiver.Path] = i
		if receiver.SecretEnv == "" && !receiver.AllowUnsigned {
			return nil, fmt.Errorf("github receiver %d: secret_env is required unless allow_unsigned is true", i)
		}
	}
	workspaceNames := make(map[string]int, len(cfg.Slack.Workspaces))
	for i, workspace := range cfg.Slack.Workspaces {
		if workspace.Name == "" {
			return nil, fmt.Errorf("slack workspace %d: name is required", i)
		}
		if prev, ok := workspaceNames[workspace.Name]; ok {
			return nil, fmt.Errorf("slack workspace %d: name duplicates workspace %d", i, prev)
		}
		workspaceNames[workspace.Name] = i
	}
	if _, err := rules.NewSet(cfg.Rules); err != nil {
		return nil, fmt.Errorf("validate rules: %w", err)
	}
	return &cfg, nil
}
