package slack_test

import (
	"context"
	"testing"
	"time"

	"github.com/mrchypark/relaker/internal/rules"
	slackrecv "github.com/mrchypark/relaker/internal/slack"
	"github.com/slack-go/slack/socketmode"
)

type ackRecorder struct {
	ids []string
}

func (a *ackRecorder) Ack(id string) error {
	a.ids = append(a.ids, id)
	return nil
}

func TestProcessorAcksEnvelopeAndNormalizesEventCallback(t *testing.T) {
	processor := slackrecv.NewProcessor()
	acks := &ackRecorder{}
	envelope := []byte(`{
	  "envelope_id":"env-1",
	  "type":"events_api",
	  "payload":{
	    "type":"event_callback",
	    "event_id":"Ev123",
	    "event":{
	      "type":"app_mention",
	      "channel":"C0123456789",
	      "user":"U123",
	      "text":"deploy staging"
	    }
	  }
	}`)

	got, ok, err := processor.ProcessEnvelope(context.Background(), envelope, acks)
	if err != nil {
		t.Fatalf("ProcessEnvelope returned error: %v", err)
	}
	if !ok {
		t.Fatal("ProcessEnvelope ok = false")
	}
	if len(acks.ids) != 1 || acks.ids[0] != "env-1" {
		t.Fatalf("acks = %#v", acks.ids)
	}
	if got.Source != "slack" || got.Event != "app_mention" || got.ID != "Ev123" || got.EnvelopeID != "env-1" {
		t.Fatalf("unexpected event identity: %#v", got)
	}
	if got.Channel != "C0123456789" || got.User != "U123" || got.Text != "deploy staging" {
		t.Fatalf("unexpected slack fields: %#v", got)
	}
	if string(got.Payload) == string(envelope) {
		t.Fatal("payload contains full socket envelope, want event_callback payload")
	}
}

func TestProcessorUsesEnvelopeIDWhenEventIDMissing(t *testing.T) {
	processor := slackrecv.NewProcessor()
	acks := &ackRecorder{}
	envelope := []byte(`{
	  "envelope_id":"env-2",
	  "type":"events_api",
	  "payload":{
	    "type":"event_callback",
	    "event":{"type":"reaction_added","reaction":"rocket","channel":"C1","user":"U1"}
	  }
	}`)

	got, ok, err := processor.ProcessEnvelope(context.Background(), envelope, acks)
	if err != nil {
		t.Fatalf("ProcessEnvelope returned error: %v", err)
	}
	if !ok {
		t.Fatal("ProcessEnvelope ok = false")
	}
	if got.ID != "env-2" || got.EnvelopeID != "env-2" {
		t.Fatalf("ids = event %q envelope %q", got.ID, got.EnvelopeID)
	}
	if got.Reaction != "rocket" {
		t.Fatalf("reaction = %q", got.Reaction)
	}
}

func TestHandleSocketModeEventAcksBeforeAsyncDispatch(t *testing.T) {
	order := make(chan string, 2)
	acks := &socketAckRecorder{order: order}
	sink := &eventSinkRecorder{
		order:  order,
		events: make(chan rules.Event, 1),
	}
	payload := []byte(`{
	  "type":"event_callback",
	  "event_id":"Ev-live",
	  "event":{
	    "type":"app_mention",
	    "channel":"C0123456789",
	    "user":"U123",
	    "text":"deploy staging"
	  }
	}`)

	handled, err := slackrecv.HandleSocketModeEvent(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{
			EnvelopeID: "env-live",
			Payload:    payload,
		},
	}, acks, sink)
	if err != nil {
		t.Fatalf("HandleSocketModeEvent returned error: %v", err)
	}
	if !handled {
		t.Fatal("HandleSocketModeEvent handled = false")
	}
	if len(acks.ids) != 1 || acks.ids[0] != "env-live" {
		t.Fatalf("acks = %#v", acks.ids)
	}
	if first := <-order; first != "ack" {
		t.Fatalf("first operation = %q, want ack", first)
	}

	select {
	case got := <-sink.events:
		if got.Source != "slack" || got.Event != "app_mention" || got.ID != "Ev-live" || got.EnvelopeID != "env-live" {
			t.Fatalf("unexpected event identity: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async dispatch")
	}
}

func TestRunSocketModeConsumesClientEventsWithoutNetwork(t *testing.T) {
	client := &fakeSocketClient{
		events: make(chan socketmode.Event, 1),
	}
	sink := &eventSinkRecorder{
		order:  make(chan string, 1),
		events: make(chan rules.Event, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- slackrecv.RunSocketMode(ctx, client, sink)
	}()

	client.events <- socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{
			EnvelopeID: "env-loop",
			Payload: []byte(`{
			  "type":"event_callback",
			  "event_id":"Ev-loop",
			  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
			}`),
		},
	}

	select {
	case got := <-sink.events:
		if got.ID != "Ev-loop" || got.EnvelopeID != "env-loop" {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for loop dispatch")
	}
	if len(client.acks) != 1 || client.acks[0] != "env-loop" {
		t.Fatalf("acks = %#v", client.acks)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunSocketMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RunSocketMode to stop")
	}
}

func TestRunSocketModeCancelsClientRunContextOnEventError(t *testing.T) {
	client := &cancellableSocketClient{
		events:  make(chan socketmode.Event, 1),
		runDone: make(chan struct{}),
	}
	sink := &eventSinkRecorder{
		order:  make(chan string, 1),
		events: make(chan rules.Event, 1),
	}
	client.events <- socketmode.Event{Type: socketmode.EventTypeEventsAPI}

	err := slackrecv.RunSocketMode(context.Background(), client, sink)
	if err == nil {
		t.Fatal("RunSocketMode returned nil error")
	}
	select {
	case <-client.runDone:
	case <-time.After(time.Second):
		t.Fatal("client RunContext was not canceled")
	}
}

type fakeSocketClient struct {
	events chan socketmode.Event
	acks   []string
}

func (c *fakeSocketClient) RunContext(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (c *fakeSocketClient) Events() <-chan socketmode.Event {
	return c.events
}

func (c *fakeSocketClient) Ack(req socketmode.Request) error {
	c.acks = append(c.acks, req.EnvelopeID)
	return nil
}

type cancellableSocketClient struct {
	events  chan socketmode.Event
	runDone chan struct{}
}

func (c *cancellableSocketClient) RunContext(ctx context.Context) error {
	<-ctx.Done()
	close(c.runDone)
	return nil
}

func (c *cancellableSocketClient) Events() <-chan socketmode.Event {
	return c.events
}

func (c *cancellableSocketClient) Ack(socketmode.Request) error {
	return nil
}

type socketAckRecorder struct {
	order chan<- string
	ids   []string
}

func (a *socketAckRecorder) Ack(req socketmode.Request) error {
	a.ids = append(a.ids, req.EnvelopeID)
	a.order <- "ack"
	return nil
}

type eventSinkRecorder struct {
	order  chan<- string
	events chan rules.Event
}

func (s *eventSinkRecorder) Handle(event rules.Event) {
	s.order <- "handle"
	s.events <- event
}
