package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/franc/nametag-cc/internal/updater"
	"github.com/franc/nametag-cc/internal/version"
)

func main() {
	fmt.Println(version.String())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	upd, err := updater.New(updater.Config{
		Owner:          "franc",
		Repo:           "nametag-cc",
		CurrentVersion: version.Version,
		Logger:         logger,
	})
	if err != nil {
		logger.Error("failed to initialise updater", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Check for an update on startup. If one is found the process is replaced
	// and restarted here — the lines below are never reached.
	// A failed check is non-fatal; being offline shouldn't stop the program.
	if err := upd.CheckAndUpdate(ctx); err != nil {
		logger.Warn("update check failed", "error", err)
	}

	logger.Info("running", "version", version.Version)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			logger.Info("heartbeat", "time", t.Format(time.RFC3339))
		}
	}
}
