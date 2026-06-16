package rules_test

import (
	"testing"

	"github.com/mrchypark/relaker/internal/rules"
)

func TestRuleSetMatchesGitHubPullRequest(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:  "github",
		Event:   "pull_request",
		Actions: []string{"opened", "synchronize", "reopened"},
		Repo:    "my-org/my-repo",
		BaseRef: "main",
		Run:     "scripts/on-pr.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:  "github",
		Event:   "pull_request",
		Action:  "opened",
		Repo:    "my-org/my-repo",
		BaseRef: "main",
	})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, skips = %#v", len(matches), skips)
	}
	if matches[0].Run != "scripts/on-pr.sh" {
		t.Fatalf("run = %q", matches[0].Run)
	}
}

func TestRuleSetMatchesReceiverAndWorkspace(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{
		{Source: "github", Receiver: "work", Event: "pull_request", Run: "scripts/on-pr.sh"},
		{Source: "slack", Workspace: "work", Event: "app_mention", Run: "scripts/on-slack.sh"},
	})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{Source: "github", Receiver: "work", Event: "pull_request"})
	if len(matches) != 1 || matches[0].Receiver != "work" {
		t.Fatalf("github matches = %#v, skips = %#v", matches, skips)
	}
	matches, skips = rs.Match(rules.Event{Source: "slack", Workspace: "work", Event: "app_mention"})
	if len(matches) != 1 || matches[0].Workspace != "work" {
		t.Fatalf("slack matches = %#v, skips = %#v", matches, skips)
	}
	matches, _ = rs.Match(rules.Event{Source: "github", Receiver: "home", Event: "pull_request"})
	if len(matches) != 0 {
		t.Fatalf("github mismatch matches = %#v", matches)
	}
}

func TestRuleSetWithoutReceiverMatchesOldStyleEvents(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source: "github",
		Event:  "pull_request",
		Run:    "scripts/on-pr.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{Source: "github", Receiver: "work", Event: "pull_request"})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, skips = %#v", len(matches), skips)
	}
}

func TestDedupeKeysIncludeReceiverAndWorkspace(t *testing.T) {
	githubKey := rules.Event{Source: "github", Receiver: "work", ID: "delivery-1"}.DedupeKeys()[0]
	slackKey := rules.Event{Source: "slack", Workspace: "work", ID: "Ev1"}.DedupeKeys()[0]
	if githubKey != "github:work:delivery-1" {
		t.Fatalf("github key = %q", githubKey)
	}
	if slackKey != "slack:work:Ev1" {
		t.Fatalf("slack key = %q", slackKey)
	}
}

func TestRuleSetSkipsGitHubBaseRefMismatch(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:  "github",
		Event:   "pull_request",
		Actions: []string{"opened"},
		Repo:    "my-org/my-repo",
		BaseRef: "main",
		Run:     "scripts/on-pr.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:  "github",
		Event:   "pull_request",
		Action:  "opened",
		Repo:    "my-org/my-repo",
		BaseRef: "release",
	})
	if len(matches) != 0 {
		t.Fatalf("matches = %d", len(matches))
	}
	if len(skips) == 0 || skips[0].Reason == "" {
		t.Fatalf("expected skip reason, got %#v", skips)
	}
}

func TestRuleSetMatchesGitHubLabelsAllAndAny(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:    "github",
		Event:     "pull_request",
		LabelsAll: []string{"ready", "ci-pass"},
		LabelsAny: []string{"deploy", "preview"},
		Run:       "scripts/on-pr.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source: "github",
		Event:  "pull_request",
		Labels: []string{"ready", "ci-pass", "preview"},
	})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, skips = %#v", len(matches), skips)
	}
}

func TestRuleSetSkipsGitHubLabelsMismatch(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:    "github",
		Event:     "pull_request",
		LabelsAll: []string{"ready", "ci-pass"},
		LabelsAny: []string{"deploy", "preview"},
		Run:       "scripts/on-pr.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source: "github",
		Event:  "pull_request",
		Labels: []string{"ready", "docs"},
	})
	if len(matches) != 0 {
		t.Fatalf("matches = %d", len(matches))
	}
	if len(skips) == 0 || skips[0].Reason == "" {
		t.Fatalf("expected skip reason, got %#v", skips)
	}
}

func TestRuleSetMatchesSlackTextRegex(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:    "slack",
		Event:     "app_mention",
		Channel:   "C0123456789",
		TextRegex: "^deploy staging",
		Run:       "scripts/deploy-staging.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:  "slack",
		Event:   "app_mention",
		Channel: "C0123456789",
		User:    "U123",
		Text:    "deploy staging now",
	})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, skips = %#v", len(matches), skips)
	}
}

func TestRuleSetMatchesSlackEventIDAndEnvelopeID(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:     "slack",
		Event:      "app_mention",
		EventID:    "Ev123",
		EnvelopeID: "env-123",
		Run:        "scripts/deploy-staging.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:     "slack",
		Event:      "app_mention",
		ID:         "Ev123",
		EnvelopeID: "env-123",
	})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, skips = %#v", len(matches), skips)
	}
}

func TestRuleSetSkipsSlackEnvelopeIDMismatch(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:     "slack",
		Event:      "app_mention",
		EnvelopeID: "env-123",
		Run:        "scripts/deploy-staging.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:     "slack",
		Event:      "app_mention",
		EnvelopeID: "env-other",
	})
	if len(matches) != 0 {
		t.Fatalf("matches = %d", len(matches))
	}
	if len(skips) == 0 || skips[0].Reason == "" {
		t.Fatalf("expected skip reason, got %#v", skips)
	}
}

func TestRuleSetSkipsSlackTextRegexMismatch(t *testing.T) {
	rs, err := rules.NewSet([]rules.Rule{{
		Source:    "slack",
		Event:     "app_mention",
		Channel:   "C0123456789",
		TextRegex: "^deploy staging",
		Run:       "scripts/deploy-staging.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}

	matches, skips := rs.Match(rules.Event{
		Source:  "slack",
		Event:   "app_mention",
		Channel: "C0123456789",
		Text:    "deploy prod",
	})
	if len(matches) != 0 {
		t.Fatalf("matches = %d", len(matches))
	}
	if len(skips) == 0 || skips[0].Reason == "" {
		t.Fatalf("expected skip reason, got %#v", skips)
	}
}
