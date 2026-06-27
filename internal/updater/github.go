package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// githubRelease is the subset of the GitHub releases/latest API response we care about.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// findAsset returns the BrowserDownloadURL of the asset whose Name matches name,
// or the empty string if no such asset exists.
func (r *githubRelease) findAsset(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// githubAsset represents a single file attached to a GitHub release.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// fetchLatestRelease calls the GitHub releases/latest API and returns the parsed response.
// A 404 response is surfaced as a distinct, descriptive error.
func fetchLatestRelease(ctx context.Context, client *http.Client, owner, repo string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nametag-updater/1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %s/%s not found or has no releases", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from GitHub API", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release JSON: %w", err)
	}
	return &release, nil
}

// parseChecksums parses a sha256sum-format file (lines of "<hash>  <filename>")
// and returns a map from filename to lowercase hex hash.
// Blank lines and lines beginning with '#' are ignored.
func parseChecksums(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Each valid line has exactly two fields separated by two spaces.
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		hash := strings.ToLower(strings.TrimSpace(parts[0]))
		name := strings.TrimSpace(parts[1])
		if hash != "" && name != "" {
			result[name] = hash
		}
	}
	return result
}
