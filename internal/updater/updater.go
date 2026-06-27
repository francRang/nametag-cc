// Package updater checks GitHub Releases for a newer version of the running
// binary and, when one is found, downloads it, verifies its checksum, replaces
// the current binary, and restarts the process.
//
// Typical usage:
//
//	u, err := updater.New(updater.Config{
//	    Owner:          "franc",
//	    Repo:           "nametag-cc",
//	    CurrentVersion: version.Version,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	go u.RunBackground(ctx, 5*time.Minute, 24*time.Hour)
package updater

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/franc/nametag-cc/internal/version"
)

// Config holds the parameters for creating an Updater.
type Config struct {
	// Owner and Repo identify the GitHub repository that hosts releases.
	Owner, Repo string
	// CurrentVersion is the version of the running binary (e.g. "1.2.3" or "dev").
	CurrentVersion string
	// HTTPClient is used for all outbound requests. Defaults to a client with a
	// 60-second timeout when nil.
	HTTPClient *http.Client
	// Logger receives structured debug/info messages. Defaults to a discard logger.
	Logger *slog.Logger
}

// Updater fetches GitHub releases and applies updates to the running binary.
type Updater struct {
	owner   string
	repo    string
	current string
	client  *http.Client
	log     *slog.Logger

	// replaceFn and restartFn are the real platform implementations by default.
	// Tests swap them for no-op stubs so no actual binary replacement occurs.
	replaceFn func(src, dst string) error
	restartFn func(path string, args []string) error
}

// New constructs an Updater from cfg. Owner, Repo, and CurrentVersion are required;
// HTTPClient and Logger receive sensible defaults when omitted.
func New(cfg Config) (*Updater, error) {
	if cfg.Owner == "" {
		return nil, errors.New("updater: Owner is required")
	}
	if cfg.Repo == "" {
		return nil, errors.New("updater: Repo is required")
	}
	if cfg.CurrentVersion == "" {
		return nil, errors.New("updater: CurrentVersion is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Updater{
		owner:     cfg.Owner,
		repo:      cfg.Repo,
		current:   cfg.CurrentVersion,
		client:    cfg.HTTPClient,
		log:       cfg.Logger,
		replaceFn: replaceBinary,
		restartFn: restart,
	}, nil
}

// CheckAndUpdate fetches the latest GitHub release and applies the update when
// the release is strictly newer than the current version. It prints user-visible
// status messages to stdout and returns nil when no update is needed.
func (u *Updater) CheckAndUpdate(ctx context.Context) error {
	release, err := fetchLatestRelease(ctx, u.client, u.owner, u.repo)
	if err != nil {
		return fmt.Errorf("fetching release: %w", err)
	}

	newer, err := version.IsNewer(u.current, release.TagName)
	if err != nil {
		return fmt.Errorf("version comparison: %w", err)
	}
	if !newer {
		u.log.Info("already up to date", "version", u.current)
		return nil
	}

	fmt.Printf("Update available: %s → %s. Downloading...\n", u.current, release.TagName)
	u.log.Info("update available", "current", u.current, "latest", release.TagName)

	if err := u.apply(ctx, release); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}
	return nil
}

// apply downloads the release asset for the current platform, optionally verifies
// its SHA-256 checksum, then replaces the running binary and restarts.
func (u *Updater) apply(ctx context.Context, release *githubRelease) error {
	assetName := u.platformAssetName()
	assetURL := release.findAsset(assetName)
	if assetURL == "" {
		return fmt.Errorf("no asset named %q in release %s", assetName, release.TagName)
	}

	tmpPath, actualHex, err := u.downloadToTemp(ctx, assetURL)
	if err != nil {
		return fmt.Errorf("downloading asset: %w", err)
	}
	defer os.Remove(tmpPath)

	checksumURL := release.findAsset("checksums.txt")
	if checksumURL == "" {
		u.log.Warn("no checksums.txt asset found; skipping checksum verification")
	} else {
		if err := u.verifyChecksum(ctx, actualHex, checksumURL, assetName); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	binPath, err := resolveBinaryPath()
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	if err := u.replaceFn(tmpPath, binPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	u.log.Info("update applied; restarting", "version", release.TagName)
	fmt.Println("Update applied. Restarting...")

	return u.restartFn(binPath, os.Args)
}

// downloadToTemp streams the resource at url into a temporary file while
// simultaneously computing its SHA-256 hash. It returns the temp file path,
// the lowercase hex-encoded hash, and any error encountered.
func (u *Updater) downloadToTemp(ctx context.Context, url string) (path, sha256hex string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("building download request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "nametag-update-*")
	if err != nil {
		return "", "", fmt.Errorf("creating temp file: %w", err)
	}
	defer tmp.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("writing download: %w", err)
	}

	return tmp.Name(), fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// verifyChecksum downloads checksumURL, parses the sha256sum file, and confirms
// that the expected hash for assetName matches actualHex (comparison is case-insensitive).
func (u *Updater) verifyChecksum(ctx context.Context, actualHex, checksumURL, assetName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("building checksum request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum download returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading checksums: %w", err)
	}

	checksums := parseChecksums(string(body))
	expected, ok := checksums[assetName]
	if !ok {
		return fmt.Errorf("asset %q not found in checksums.txt", assetName)
	}
	if !strings.EqualFold(actualHex, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actualHex, expected)
	}
	return nil
}

// resolveBinaryPath returns the absolute, symlink-resolved path of the running binary.
func resolveBinaryPath() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("looking up binary path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return "", fmt.Errorf("resolving symlinks for %q: %w", bin, err)
	}
	return resolved, nil
}

// platformAssetName returns the expected GitHub release asset name for the
// current OS and architecture, e.g. "nametag-linux-amd64" or
// "nametag-windows-amd64.exe".
func (u *Updater) platformAssetName() string {
	name := fmt.Sprintf("nametag-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
