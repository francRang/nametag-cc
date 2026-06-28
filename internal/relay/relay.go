// Package relay bridges GitHub release webhooks to connected clients via
// Server-Sent Events (SSE). A Broker manages client subscriptions;
// WebhookHandler validates inbound GitHub payloads and triggers broadcasts;
// EventsHandler streams release notifications to long-lived HTTP clients.
package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Broker manages a set of SSE client channels and broadcasts update signals to
// all of them when a new GitHub release is published.
type Broker struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
}

// NewBroker returns an initialised Broker ready to accept subscribers.
func NewBroker() *Broker {
	return &Broker{clients: make(map[chan struct{}]struct{})}
}

// subscribe creates a buffered channel, registers it, and returns it.
// The buffer size of 1 means Broadcast can enqueue one notification without
// blocking, so a slow EventsHandler goroutine does not stall the webhook path.
func (b *Broker) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// unsubscribe removes ch from the client set.
func (b *Broker) unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// Broadcast sends a notification to every registered client channel.
// Clients whose channel buffer is already full are skipped rather than blocked,
// preventing one slow consumer from delaying the rest.
// Returns the count of clients successfully notified.
func (b *Broker) Broadcast() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for ch := range b.clients {
		select {
		case ch <- struct{}{}:
			n++
		default:
		}
	}
	return n
}

// ClientCount returns the number of currently connected SSE clients.
func (b *Broker) ClientCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

// VerifySignature validates the X-Hub-Signature-256 header that GitHub attaches
// to every webhook delivery. The expected format is "sha256=<hex>".
// Returns false when the prefix is absent, the hex is malformed, or the HMAC
// does not match. Uses hmac.Equal for constant-time comparison to prevent
// timing-oracle attacks.
func VerifySignature(secret, body []byte, sigHeader string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}
	got, err := hex.DecodeString(sigHeader[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// releasePayload is the minimal subset of a GitHub release webhook body we
// inspect. Only "published" actions trigger a broadcast.
type releasePayload struct {
	Action string `json:"action"`
}

// WebhookHandler returns an http.HandlerFunc for POST /webhook.
// It validates the GitHub HMAC signature, ignores non-release events and
// non-published actions, then broadcasts to all connected SSE clients.
func WebhookHandler(secret []byte, b *Broker, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cap body size before reading to prevent memory exhaustion.
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}

		if !VerifySignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			log.Warn("webhook rejected: invalid signature", "remote", r.RemoteAddr)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		// GitHub sends many event types; we only act on release events.
		if r.Header.Get("X-GitHub-Event") != "release" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var payload releasePayload
		if err := json.Unmarshal(body, &payload); err != nil || payload.Action != "published" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		n := b.Broadcast()
		log.Info("release published; notified clients", "count", n)
		w.WriteHeader(http.StatusNoContent)
	}
}

// EventsHandler returns an http.HandlerFunc for GET /events.
// It upgrades the connection to an SSE stream and forwards release
// notifications from the Broker until the client disconnects.
func EventsHandler(b *Broker, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := b.subscribe()
		defer b.unsubscribe(ch)

		log.Info("client connected", "remote", r.RemoteAddr)

		// Flushing the initial comment confirms to the client that the
		// subscription is live. Tests wait for this line before broadcasting
		// to eliminate the subscribe/broadcast race.
		io.WriteString(w, ": connected\n\n") //nolint:errcheck
		flusher.Flush()

		// Keep-alive comments prevent proxies and load balancers from closing
		// idle connections.
		keepalive := time.NewTicker(30 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				log.Info("client disconnected", "remote", r.RemoteAddr)
				return
			case <-keepalive.C:
				io.WriteString(w, ": keepalive\n\n") //nolint:errcheck
				flusher.Flush()
			case <-ch:
				io.WriteString(w, "data: update\n\n") //nolint:errcheck
				flusher.Flush()
			}
		}
	}
}
