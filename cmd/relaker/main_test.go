package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrchypark/relaker/internal/dedupe"
	"github.com/mrchypark/relaker/internal/gateway"
	"github.com/mrchypark/relaker/internal/rules"
	"github.com/mrchypark/relaker/internal/runner"
)

type captureMainSink struct {
	event rules.Event
}

func (s *captureMainSink) Handle(event rules.Event) {
	s.event = event
}

func TestReceiverSinkAddsReceiver(t *testing.T) {
	sink := &captureMainSink{}

	receiverSink{name: "work", sink: sink}.Handle(rules.Event{Source: "github", Event: "pull_request"})

	if sink.event.Receiver != "work" {
		t.Fatalf("receiver = %q", sink.event.Receiver)
	}
}

func TestWorkspaceSinkAddsWorkspace(t *testing.T) {
	sink := &captureMainSink{}

	workspaceSink{name: "work", sink: sink}.Handle(rules.Event{Source: "slack", Event: "app_mention"})

	if sink.event.Workspace != "work" {
		t.Fatalf("workspace = %q", sink.event.Workspace)
	}
}

func TestProcessSlackEnvelopeAppliesWorkspace(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	scriptPath := filepath.Join(dir, "scripts", "deploy.sh")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
printf '%s|%s|%s\n' "$RELAKER_SOURCE" "$RELAKER_EVENT" "$RELAKER_SLACK_TEXT" > "` + out + `"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	envelopePath := filepath.Join(dir, "envelope.json")
	envelope := []byte(`{
	  "envelope_id":"env-1",
	  "type":"events_api",
	  "payload":{
	    "type":"event_callback",
	    "event_id":"Ev123",
	    "event":{"type":"app_mention","channel":"C0123456789","text":"deploy staging"}
	  }
	}`)
	if err := os.WriteFile(envelopePath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	rs, err := rules.NewSet([]rules.Rule{{
		Source:    "slack",
		Workspace: "work",
		Event:     "app_mention",
		TextRegex: "^deploy staging",
		Run:       "scripts/deploy.sh",
	}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(dir, rs.Runs())
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(rs, dedupe.NewMemoryStore(), r)

	if err := processSlackEnvelope(context.Background(), envelopePath, "work", gw); err != nil {
		t.Fatalf("processSlackEnvelope returned error: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "slack|app_mention|deploy staging") {
		t.Fatalf("output = %q", string(got))
	}
}
