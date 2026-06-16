package slack

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mrchypark/relaker/internal/rules"
	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

type Acker interface {
	Ack(envelopeID string) error
}

type Processor struct{}

type EventSink interface {
	Handle(rules.Event)
}

type SocketAcker interface {
	Ack(socketmode.Request) error
}

type SocketClient interface {
	RunContext(context.Context) error
	Events() <-chan socketmode.Event
	SocketAcker
}

func NewProcessor() *Processor {
	return &Processor{}
}

func (p *Processor) ProcessEnvelope(_ context.Context, data []byte, acker Acker) (rules.Event, bool, error) {
	var envelope struct {
		EnvelopeID string          `json:"envelope_id"`
		Type       string          `json:"type"`
		Payload    json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return rules.Event{}, false, fmt.Errorf("decode socket envelope: %w", err)
	}
	if acker != nil && envelope.EnvelopeID != "" {
		if err := acker.Ack(envelope.EnvelopeID); err != nil {
			return rules.Event{}, false, fmt.Errorf("ack envelope: %w", err)
		}
	}

	return normalizePayload(envelope.Payload, envelope.EnvelopeID, envelope.Payload)
}

func HandleSocketModeEvent(_ context.Context, event socketmode.Event, acker SocketAcker, sink EventSink) (bool, error) {
	if event.Type != socketmode.EventTypeEventsAPI {
		return false, nil
	}
	if event.Request == nil {
		return false, fmt.Errorf("socket mode event missing request")
	}
	if acker != nil {
		if err := acker.Ack(*event.Request); err != nil {
			return false, fmt.Errorf("ack socket mode envelope: %w", err)
		}
	}
	normalized, ok, err := normalizePayload(event.Request.Payload, event.Request.EnvelopeID, event.Request.Payload)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	sink.Handle(normalized)
	return true, nil
}

func NewSocketModeClient(appToken, botToken string) SocketClient {
	api := slackapi.New(botToken, slackapi.OptionAppLevelToken(appToken))
	return socketModeClient{client: socketmode.New(api)}
}

func RunSocketMode(ctx context.Context, client SocketClient, sink EventSink) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.RunContext(runCtx)
	}()
	for {
		select {
		case event, ok := <-client.Events():
			if !ok {
				return nil
			}
			if _, err := HandleSocketModeEvent(ctx, event, client, sink); err != nil {
				return err
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

type socketModeClient struct {
	client *socketmode.Client
}

func (c socketModeClient) RunContext(ctx context.Context) error {
	return c.client.RunContext(ctx)
}

func (c socketModeClient) Events() <-chan socketmode.Event {
	return c.client.Events
}

func (c socketModeClient) Ack(req socketmode.Request) error {
	return c.client.Ack(req)
}

func normalizePayload(payload json.RawMessage, envelopeID string, raw []byte) (rules.Event, bool, error) {
	var callback struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
		Event   struct {
			Type     string `json:"type"`
			Channel  string `json:"channel"`
			User     string `json:"user"`
			Text     string `json:"text"`
			Reaction string `json:"reaction"`
			Item     struct {
				Channel string `json:"channel"`
			} `json:"item"`
		} `json:"event"`
	}
	if err := json.Unmarshal(payload, &callback); err != nil {
		return rules.Event{}, false, fmt.Errorf("decode event_callback: %w", err)
	}
	if callback.Type != "event_callback" {
		return rules.Event{}, false, nil
	}
	id := callback.EventID
	if id == "" {
		id = envelopeID
	}
	channel := callback.Event.Channel
	if channel == "" {
		channel = callback.Event.Item.Channel
	}
	return rules.Event{
		Source:     "slack",
		Event:      callback.Event.Type,
		ID:         id,
		EnvelopeID: envelopeID,
		Channel:    channel,
		User:       callback.Event.User,
		Text:       callback.Event.Text,
		Reaction:   callback.Event.Reaction,
		Payload:    append([]byte(nil), raw...),
	}, true, nil
}
