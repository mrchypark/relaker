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

func TestProcessorSkipsNonEventsAPIEnvelope(t *testing.T) {
	processor := slackrecv.NewProcessor()
	acks := &ackRecorder{}
	envelope := []byte(`{"type":"hello"}`)

	got, ok, err := processor.ProcessEnvelope(context.Background(), envelope, acks)
	if err != nil {
		t.Fatalf("ProcessEnvelope returned error: %v", err)
	}
	if ok {
		t.Fatalf("ProcessEnvelope ok = true, event = %#v", got)
	}
	if len(acks.ids) != 0 {
		t.Fatalf("acks = %#v", acks.ids)
	}
}

func TestHandleSocketModeEventReturnsBeforeBlockingSinkCompletesAfterAck(t *testing.T) {
	order := make(chan string, 2)
	acks := &socketAckRecorder{order: order}
	sink := &blockingEventSink{
		started:  make(chan rules.Event, 1),
		release:  make(chan struct{}),
		finished: make(chan struct{}, 1),
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

	done := make(chan struct {
		handled bool
		err     error
	}, 1)
	go func() {
		handled, err := slackrecv.HandleSocketModeEvent(context.Background(), socketmode.Event{
			Type: socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{
				EnvelopeID: "env-live",
				Payload:    payload,
			},
		}, acks, sink)
		done <- struct {
			handled bool
			err     error
		}{handled: handled, err: err}
	}()

	if first := <-order; first != "ack" {
		t.Fatalf("first operation = %q, want ack", first)
	}
	if len(acks.ids) != 1 || acks.ids[0] != "env-live" {
		t.Fatalf("acks = %#v", acks.ids)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("HandleSocketModeEvent returned error: %v", result.err)
		}
		if !result.handled {
			t.Fatal("HandleSocketModeEvent handled = false")
		}
	case <-time.After(100 * time.Millisecond):
		close(sink.release)
		<-done
		t.Fatal("HandleSocketModeEvent did not return before sink.Handle completed")
	}

	select {
	case <-sink.finished:
		t.Fatal("sink.Handle completed before release")
	default:
	}
	close(sink.release)
	select {
	case got := <-sink.started:
		if got.Source != "slack" || got.Event != "app_mention" || got.ID != "Ev-live" || got.EnvelopeID != "env-live" {
			t.Fatalf("unexpected event identity: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sink to start")
	}
	select {
	case <-sink.finished:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sink.Handle to finish")
	}
}

func TestHandleSocketModeEventAcksUnsupportedRequest(t *testing.T) {
	acks := &socketAckRecorder{order: make(chan string, 1)}
	handled, err := slackrecv.HandleSocketModeEvent(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeSlashCommand,
		Request: &socketmode.Request{
			EnvelopeID: "env-unsupported",
		},
	}, acks, &eventSinkRecorder{order: make(chan string, 1), events: make(chan rules.Event, 1)})
	if err != nil {
		t.Fatalf("HandleSocketModeEvent returned error: %v", err)
	}
	if handled {
		t.Fatal("unsupported event handled = true")
	}
	if len(acks.ids) != 1 || acks.ids[0] != "env-unsupported" {
		t.Fatalf("acks = %#v", acks.ids)
	}
}

func TestHandleSocketModeEventAppliesDispatchBackpressure(t *testing.T) {
	handler := slackrecv.NewEventHandler(16)
	release := make(chan struct{})
	for i := 0; i < 16; i++ {
		sink := &blockingEventSink{
			started:  make(chan rules.Event, 1),
			release:  release,
			finished: make(chan struct{}, 1),
		}
		handled, err := handler.HandleSocketModeEvent(context.Background(), socketmode.Event{
			Type: socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{
				EnvelopeID: "env-fill",
				Payload: []byte(`{
				  "type":"event_callback",
				  "event_id":"Ev-fill",
				  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
				}`),
			},
		}, &socketAckRecorder{order: make(chan string, 1)}, sink)
		if err != nil || !handled {
			t.Fatalf("fill dispatch %d handled=%v err=%v", i, handled, err)
		}
		select {
		case <-sink.started:
		case <-time.After(time.Second):
			t.Fatalf("dispatch %d did not start", i)
		}
	}

	busySink := &eventSinkRecorder{
		order:  make(chan string, 1),
		events: make(chan rules.Event, 1),
	}
	done := make(chan struct{}, 1)
	go func() {
		_, _ = handler.HandleSocketModeEvent(context.Background(), socketmode.Event{
			Type: socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{
				EnvelopeID: "env-blocked",
				Payload: []byte(`{
				  "type":"event_callback",
				  "event_id":"Ev-blocked",
				  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
				}`),
			},
		}, &socketAckRecorder{order: make(chan string, 1)}, busySink)
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not return while all slots were full")
	}
	select {
	case got := <-busySink.events:
		t.Fatalf("busy sink handled despite full dispatch slots: %#v", got)
	default:
	}
	close(release)
	select {
	case got := <-busySink.events:
		t.Fatalf("busy sink handled after slot release: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandleSocketModeEventStopsWaitingForDispatchSlotWhenContextCancels(t *testing.T) {
	handler := slackrecv.NewEventHandler(16)
	release := make(chan struct{})
	for i := 0; i < 16; i++ {
		sink := &blockingEventSink{
			started:  make(chan rules.Event, 1),
			release:  release,
			finished: make(chan struct{}, 1),
		}
		handled, err := handler.HandleSocketModeEvent(context.Background(), socketmode.Event{
			Type: socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{
				EnvelopeID: "env-fill-cancel",
				Payload: []byte(`{
				  "type":"event_callback",
				  "event_id":"Ev-fill-cancel",
				  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
				}`),
			},
		}, &socketAckRecorder{order: make(chan string, 1)}, sink)
		if err != nil || !handled {
			t.Fatalf("fill dispatch %d handled=%v err=%v", i, handled, err)
		}
		select {
		case <-sink.started:
		case <-time.After(time.Second):
			t.Fatalf("dispatch %d did not start", i)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handled, err := handler.HandleSocketModeEvent(ctx, socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{
			EnvelopeID: "env-canceled",
			Payload: []byte(`{
			  "type":"event_callback",
			  "event_id":"Ev-canceled",
			  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
			}`),
		},
	}, &socketAckRecorder{order: make(chan string, 1)}, &eventSinkRecorder{
		order:  make(chan string, 1),
		events: make(chan rules.Event, 1),
	})
	if err == nil {
		t.Fatal("HandleSocketModeEvent returned nil error for canceled context")
	}
	if handled {
		t.Fatal("canceled dispatch handled = true")
	}
	close(release)
}

func TestHandleSocketModeEventRecoversDispatchPanic(t *testing.T) {
	sink := panicEventSink{started: make(chan rules.Event, 1)}
	handled, err := slackrecv.NewEventHandler(1).HandleSocketModeEvent(context.Background(), socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{
			EnvelopeID: "env-panic",
			Payload: []byte(`{
			  "type":"event_callback",
			  "event_id":"Ev-panic",
			  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
			}`),
		},
	}, &socketAckRecorder{order: make(chan string, 1)}, sink)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	select {
	case got := <-sink.started:
		if got.ID != "Ev-panic" {
			t.Fatalf("event = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for panic sink")
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

func TestRunSocketModeContinuesAfterBadEvent(t *testing.T) {
	client := &cancellableSocketClient{
		events:  make(chan socketmode.Event, 2),
		runDone: make(chan struct{}),
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

	client.events <- socketmode.Event{Type: socketmode.EventTypeEventsAPI}
	client.events <- socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{
			EnvelopeID: "env-after-bad",
			Payload: []byte(`{
			  "type":"event_callback",
			  "event_id":"Ev-after-bad",
			  "event":{"type":"app_mention","channel":"C1","user":"U1","text":"deploy staging"}
			}`),
		},
	}

	select {
	case got := <-sink.events:
		if got.ID != "Ev-after-bad" {
			t.Fatalf("unexpected event after bad payload: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event after bad payload")
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

type blockingEventSink struct {
	started  chan rules.Event
	release  chan struct{}
	finished chan struct{}
}

func (s *blockingEventSink) Handle(event rules.Event) {
	s.started <- event
	<-s.release
	s.finished <- struct{}{}
}

type panicEventSink struct {
	started chan rules.Event
}

func (s panicEventSink) Handle(event rules.Event) {
	s.started <- event
	panic("handler panic")
}
