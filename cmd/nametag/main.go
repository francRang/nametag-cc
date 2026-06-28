package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	gocron "github.com/netresearch/go-cron"

	"github.com/franc/nametag-cc/internal/updater"
	"github.com/franc/nametag-cc/internal/version"
)

func main() {
	interval := flag.Duration("interval", time.Hour, "how often to poll for updates (e.g. 30s, 5m, 1h)")
	cronExpr := flag.String("cron", "", `cron expression for update schedule (e.g. "0 * * * *")`)
	relayURL := flag.String("relay", "", "relay server URL for instant update notifications (e.g. http://relay:8080)")
	flag.Parse()

	// Detect which flags were explicitly provided so we can enforce mutual exclusion.
	// flag.Visit only visits flags that were set on the command line.
	var intervalSet, cronSet bool
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "interval":
			intervalSet = true
		case "cron":
			cronSet = true
		}
	})
	if intervalSet && cronSet {
		fmt.Fprintln(os.Stderr, "error: -interval and -cron are mutually exclusive")
		os.Exit(1)
	}

	// Validate the cron expression before doing anything else so the user gets
	// a clear error immediately rather than after the startup update check.
	if cronSet {
		if err := gocron.ValidateSpec(*cronExpr); err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid cron expression %q: %v\n", *cronExpr, err)
			os.Exit(1)
		}
	}

	fmt.Println(version.String())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	upd, err := updater.New(updater.Config{
		Owner:          "francRang",
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
	// and restarted here; the lines below are never reached.
	// A failed check is non-fatal; being offline shouldn't stop the program.
	if err := upd.CheckAndUpdate(ctx); err != nil {
		logger.Warn("startup update check failed", "error", err)
	}

	// Schedule background checks using whichever mode was requested.
	if cronSet {
		c := gocron.New(gocron.WithLogger(gocron.NewSlogLogger(logger)))
		if _, err := c.AddFunc(*cronExpr, func() {
			if err := upd.CheckAndUpdate(ctx); err != nil {
				logger.Warn("background update check failed", "error", err)
			}
		}); err != nil {
			logger.Error("failed to register cron job", "error", err)
			os.Exit(1)
		}
		c.Start()
		defer c.StopAndWait()
		logger.Info("running", "version", version.Version, "update_schedule", *cronExpr)
	} else {
		upd.RunBackground(ctx, *interval)
		logger.Info("running", "version", version.Version, "update_interval", interval)
	}

	if *relayURL != "" {
		subscribeRelay(ctx, *relayURL, logger, func() {
			// The webhook fires when the release is published, but CI still needs
			// time to build and upload the binaries. Retry with a fixed delay to
			// give the pipeline time to finish.
			delays := []time.Duration{0, time.Minute, time.Minute, time.Minute, time.Minute, time.Minute, time.Minute, time.Minute, time.Minute}
			for attempt, delay := range delays {
				if delay > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
				}
				if err := upd.CheckAndUpdate(ctx); err != nil {
					logger.Warn("relay-triggered update check failed",
						"error", err,
						"attempt", attempt+1,
						"of", len(delays))
					if attempt+1 < len(delays) {
						logger.Info("retrying in 1m")
					}
					continue
				}
				return
			}
			logger.Warn("relay-triggered update exhausted retries; will catch up on next poll interval")
		})
		logger.Info("relay subscription active", "url", *relayURL)
	}

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
