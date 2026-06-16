package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/mrchypark/relaker/internal/rules"
)

type Sink interface {
	Handle(rules.Event)
}

type asyncSink interface {
	HandleAsync(rules.Event, func())
}

type Handler struct {
	secret string
	sink   Sink
	logger *log.Logger
	sem    chan struct{}
}

const maxBodyBytes = 5 << 20
const maxDispatches = 16

func NewHandler(secret string, sink Sink) *Handler {
	return &Handler{secret: secret, sink: sink, logger: log.Default(), sem: make(chan struct{}, maxDispatches)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if h.secret != "" && !validSignature(body, h.secret, r.Header.Get("X-Hub-Signature-256")) {
		h.logger.Printf("stage=verify result=reject source=github reason=bad_signature")
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery == "" {
		http.Error(w, "missing delivery", http.StatusBadRequest)
		return
	}
	eventName := r.Header.Get("X-GitHub-Event")
	if eventName == "" {
		http.Error(w, "missing event", http.StatusBadRequest)
		return
	}
	event, err := normalize(body, eventName, delivery)
	if err != nil {
		http.Error(w, "parse payload", http.StatusBadRequest)
		return
	}
	select {
	case h.sem <- struct{}{}:
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	release := func() { <-h.sem }
	if sink, ok := h.sink.(asyncSink); ok {
		sink.HandleAsync(event, release)
		return
	}
	go func() {
		defer func() {
			release()
			if recovered := recover(); recovered != nil {
				h.logger.Printf("stage=dispatch result=panic source=github event=%s id=%s panic=%v", event.Event, event.ID, recovered)
			}
		}()
		h.sink.Handle(event)
	}()
}

func validSignature(body []byte, secret, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(header))
}

func normalize(body []byte, eventName, delivery string) (rules.Event, error) {
	var payload struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"pull_request"`
		Issue struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return rules.Event{}, fmt.Errorf("decode github payload: %w", err)
	}
	labels := make([]string, 0, len(payload.PullRequest.Labels)+len(payload.Issue.Labels))
	for _, label := range payload.PullRequest.Labels {
		if label.Name != "" {
			labels = append(labels, label.Name)
		}
	}
	for _, label := range payload.Issue.Labels {
		if label.Name != "" {
			labels = append(labels, label.Name)
		}
	}
	return rules.Event{
		Source:  "github",
		Event:   eventName,
		Action:  payload.Action,
		ID:      delivery,
		Repo:    payload.Repository.FullName,
		BaseRef: payload.PullRequest.Base.Ref,
		Labels:  labels,
		Payload: append([]byte(nil), body...),
	}, nil
}
