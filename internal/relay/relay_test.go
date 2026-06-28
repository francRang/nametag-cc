package relay

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// sign returns the "sha256=<hex>" HMAC signature for body using secret.
func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	secret := []byte("testsecret")
	body := []byte(`{"action":"published"}`)

	tests := []struct {
		name   string
		secret []byte
		sig    string
		want   bool
	}{
		{"valid", secret, sign(secret, body), true},
		{"wrong secret", []byte("wrongsecret"), sign(secret, body), false},
		{"missing prefix", secret, hex.EncodeToString([]byte("nope")), false},
		{"non-hex value", secret, "sha256=notvalidhex!!", false},
		{"empty", secret, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(tc.secret, body, tc.sig); got != tc.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBroker_Broadcast_NoClients(t *testing.T) {
	b := NewBroker()
	if n := b.Broadcast(); n != 0 {
		t.Errorf("Broadcast() with no clients = %d, want 0", n)
	}
}

func TestBroker_Broadcast_NotifiesSubscribers(t *testing.T) {
	b := NewBroker()
	ch1 := b.subscribe()
	ch2 := b.subscribe()

	if n := b.Broadcast(); n != 2 {
		t.Errorf("Broadcast() = %d, want 2", n)
	}
	for i, ch := range []chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		default:
			t.Errorf("ch%d did not receive a notification", i+1)
		}
	}
}

func TestBroker_Broadcast_SkipsSlowClients(t *testing.T) {
	b := NewBroker()
	slow := b.subscribe()
	fast := b.subscribe()

	// Pre-fill slow's single-slot buffer so Broadcast cannot enqueue into it.
	slow <- struct{}{}

	if n := b.Broadcast(); n != 1 {
		t.Errorf("Broadcast() = %d, want 1 (slow client must be skipped)", n)
	}
	select {
	case <-fast:
	default:
		t.Error("fast client did not receive notification")
	}
}

func TestBroker_ClientCount(t *testing.T) {
	b := NewBroker()
	if c := b.ClientCount(); c != 0 {
		t.Errorf("initial ClientCount() = %d, want 0", c)
	}
	ch1 := b.subscribe()
	ch2 := b.subscribe()
	if c := b.ClientCount(); c != 2 {
		t.Errorf("ClientCount() after 2 subscribes = %d, want 2", c)
	}
	b.unsubscribe(ch1)
	if c := b.ClientCount(); c != 1 {
		t.Errorf("ClientCount() after 1 unsubscribe = %d, want 1", c)
	}
	b.unsubscribe(ch2)
	if c := b.ClientCount(); c != 0 {
		t.Errorf("ClientCount() after all unsubscribes = %d, want 0", c)
	}
}

func TestWebhookHandler_ValidPublishedRelease(t *testing.T) {
	secret := []byte("testsecret")
	b := NewBroker()

	// Subscribe before invoking the handler so the broadcast has a receiver.
	ch := b.subscribe()
	defer b.unsubscribe(ch)

	body := []byte(`{"action":"published"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	req.Header.Set("X-GitHub-Event", "release")
	rr := httptest.NewRecorder()

	WebhookHandler(secret, b, discardLogger())(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
	select {
	case <-ch:
	default:
		t.Error("expected broadcast to notify subscribed channel")
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	secret := []byte("testsecret")
	b := NewBroker()

	body := []byte(`{"action":"published"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	req.Header.Set("X-GitHub-Event", "release")
	rr := httptest.NewRecorder()

	WebhookHandler(secret, b, discardLogger())(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestWebhookHandler_WrongEvent(t *testing.T) {
	secret := []byte("testsecret")
	b := NewBroker()
	ch := b.subscribe()
	defer b.unsubscribe(ch)

	body := []byte(`{"action":"published"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	req.Header.Set("X-GitHub-Event", "push")
	rr := httptest.NewRecorder()

	WebhookHandler(secret, b, discardLogger())(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
	select {
	case <-ch:
		t.Error("expected no broadcast for non-release event")
	default:
	}
}

func TestWebhookHandler_WrongAction(t *testing.T) {
	secret := []byte("testsecret")
	b := NewBroker()
	ch := b.subscribe()
	defer b.unsubscribe(ch)

	body := []byte(`{"action":"created"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	req.Header.Set("X-GitHub-Event", "release")
	rr := httptest.NewRecorder()

	WebhookHandler(secret, b, discardLogger())(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
	select {
	case <-ch:
		t.Error("expected no broadcast for non-published action")
	default:
	}
}

func TestEventsHandler_ReceivesNotification(t *testing.T) {
	b := NewBroker()
	srv := httptest.NewServer(EventsHandler(b, discardLogger()))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	// Wait for ": connected" before broadcasting to eliminate the
	// subscribe/broadcast race — EventsHandler flushes this only after
	// subscribe() has returned.
	deadline := time.After(2 * time.Second)
waitConnected:
	for {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, ": connected") {
				break waitConnected
			}
		case <-deadline:
			t.Fatal("timed out waiting for ': connected' comment")
		}
	}

	b.Broadcast()

	deadline = time.After(2 * time.Second)
	for {
		select {
		case line := <-lines:
			if strings.HasPrefix(line, "data:") {
				return // success
			}
		case <-deadline:
			t.Fatal("timed out waiting for 'data:' line after broadcast")
		}
	}
}
