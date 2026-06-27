// Package version exposes the application version and semver comparison helpers.
//
// The Version variable is intended to be overridden at build time via linker flags:
//
//	go build -ldflags "-X github.com/franc/nametag-cc/internal/version.Version=1.2.3" .
//
// When no flag is supplied the variable retains its default value of "dev", which
// signals a local development build and disables automatic update checks.
package version

import (
	"fmt"

	"golang.org/x/mod/semver"
)

const AppName = "Awesome program"

// Version is set at build time via -ldflags. The default "dev" disables update checks.
var Version = "dev"

// String returns a human-readable version string, e.g. "Awesome program version: 1.2.3".
func String() string {
	return fmt.Sprintf("%s version: %s", AppName, Version)
}

// IsNewer reports whether candidate is a strictly newer semver release than current.
//
// Rules:
//   - If current is "dev", always returns (false, nil): dev builds are never auto-updated.
//   - Both current and candidate may omit the leading "v"; it is added automatically.
//   - Any string that is not valid semver (after normalization) causes an error.
func IsNewer(current, candidate string) (bool, error) {
	// Dev builds skip the update check entirely — no need to validate semver.
	if current == "dev" {
		return false, nil
	}

	nc := normalize(current)
	if !semver.IsValid(nc) {
		return false, fmt.Errorf("invalid semver for current version: %q", current)
	}

	nca := normalize(candidate)
	if !semver.IsValid(nca) {
		return false, fmt.Errorf("invalid semver for candidate version: %q", candidate)
	}

	return semver.Compare(nca, nc) > 0, nil
}

// normalize ensures v has the "v" prefix required by golang.org/x/mod/semver.
func normalize(v string) string {
	if len(v) > 0 && v[0] != 'v' {
		return "v" + v
	}
	return v
}
