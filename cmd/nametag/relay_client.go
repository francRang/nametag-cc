package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// subscribeRelay connects to the relay server and calls onUpdate whenever a
// release notification arrives. It reconnects automatically if the connection
// drops. Runs in a background goroutine; exits when ctx is cancelled.
func subscribeRelay(ctx context.Context, relayURL string, log *slog.Logger, onUpdate func()) {
	go func() {
		for {
			if err := listenSSE(ctx, relayURL, onUpdate); err != nil && ctx.Err() == nil {
				log.Warn("relay connection lost; reconnecting in 5s", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

// listenSSE opens a single SSE connection to relayURL/events and calls onUpdate
// for each "data:" line received. Returns when the connection closes or ctx is
// cancelled.
func listenSSE(ctx context.Context, relayURL string, onUpdate func()) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, relayURL+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			onUpdate()
		}
	}
	return scanner.Err()
}
