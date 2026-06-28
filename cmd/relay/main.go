// Command relay is the update notification server. It receives GitHub release
// webhooks and immediately notifies all connected nametag clients via
// Server-Sent Events, triggering instant self-updates without polling.
//
// Usage:
//
//	WEBHOOK_SECRET=<secret> relay -addr :8080
//
// Configure a GitHub webhook pointing to https://<host>/webhook with:
//   - Content type: application/json
//   - Events: Releases only
//   - Secret: the value of WEBHOOK_SECRET
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/franc/nametag-cc/internal/relay"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	// Read the secret from the environment rather than a flag so it never
	// appears in process listings or shell history.
	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "error: WEBHOOK_SECRET environment variable is not set")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	b := relay.NewBroker()

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", relay.WebhookHandler([]byte(secret), b, logger))
	mux.HandleFunc("/events", relay.EventsHandler(b, logger))

	logger.Info("relay listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
