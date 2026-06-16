package github_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	githubrecv "github.com/mrchypark/relaker/internal/github"
	"github.com/mrchypark/relaker/internal/rules"
)

type captureSink struct {
	events chan rules.Event
}

func (s captureSink) Handle(event rules.Event) {
	s.events <- event
}

func TestHandlerVerifiesSignatureAndDispatchesPullRequestAsync(t *testing.T) {
	body := []byte(`{
	  "action":"opened",
	  "repository":{"full_name":"my-org/my-repo"},
	  "pull_request":{"base":{"ref":"main"},"labels":[{"name":"ready"}]}
	}`)
	sink := captureSink{events: make(chan rules.Event, 1)}
	handler := githubrecv.NewHandler("secret", sink)

	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", "sha256="+sign(body, "secret"))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}

	select {
	case got := <-sink.events:
		if got.Source != "github" || got.Event != "pull_request" || got.Action != "opened" {
			t.Fatalf("unexpected event identity: %#v", got)
		}
		if got.ID != "delivery-1" || got.Repo != "my-org/my-repo" || got.BaseRef != "main" {
			t.Fatalf("unexpected normalized event: %#v", got)
		}
		if len(got.Labels) != 1 || got.Labels[0] != "ready" {
			t.Fatalf("labels = %#v", got.Labels)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async dispatch")
	}
}

func TestHandlerRejectsInvalidSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	sink := captureSink{events: make(chan rules.Event, 1)}
	handler := githubrecv.NewHandler("secret", sink)

	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
	select {
	case got := <-sink.events:
		t.Fatalf("unexpected dispatch: %#v", got)
	default:
	}
}

func TestHandlerRejectsOversizedBody(t *testing.T) {
	sink := captureSink{events: make(chan rules.Event, 1)}
	handler := githubrecv.NewHandler("", sink)

	req := httptest.NewRequest(http.MethodPost, "/github", strings.NewReader(strings.Repeat("x", 6<<20)))
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
}

func TestHandlerReturnsUnavailableWhenDispatchSaturated(t *testing.T) {
	sink := blockingSink{started: make(chan struct{}, 32), release: make(chan struct{})}
	handler := githubrecv.NewHandler("", sink)
	body := []byte(`{"action":"opened"}`)

	for i := 0; i < 16; i++ {
		req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "issues")
		req.Header.Set("X-GitHub-Delivery", "delivery-"+string(rune('a'+i)))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("request %d status = %d", i, rr.Code)
		}
		select {
		case <-sink.started:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for dispatch %d", i)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-busy")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	close(sink.release)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
}

func TestHandlerRejectsMissingDelivery(t *testing.T) {
	sink := captureSink{events: make(chan rules.Event, 1)}
	handler := githubrecv.NewHandler("", sink)

	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader([]byte(`{"action":"opened"}`)))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
}

func TestHandlerRejectsMissingEvent(t *testing.T) {
	sink := captureSink{events: make(chan rules.Event, 1)}
	handler := githubrecv.NewHandler("", sink)

	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader([]byte(`{"action":"opened"}`)))
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
	select {
	case got := <-sink.events:
		t.Fatalf("unexpected dispatch: %#v", got)
	default:
	}
}

func TestHandlerRecoversDispatchPanic(t *testing.T) {
	sink := panicSink{started: make(chan struct{}, 1)}
	handler := githubrecv.NewHandler("", sink)
	req := httptest.NewRequest(http.MethodPost, "/github", bytes.NewReader([]byte(`{"action":"opened"}`)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %q", rr.Code, rr.Body.String())
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for panic sink")
	}
}

type blockingSink struct {
	started chan struct{}
	release chan struct{}
}

func (s blockingSink) Handle(rules.Event) {
	s.started <- struct{}{}
	<-s.release
}

type panicSink struct {
	started chan struct{}
}

func (s panicSink) Handle(rules.Event) {
	s.started <- struct{}{}
	panic("handler panic")
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
