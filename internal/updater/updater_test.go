package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// roundTripFunc implements http.RoundTripper via a plain function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// redirectingClient returns an *http.Client whose transport rewrites every
// request's host to that of srv. This lets the production URL-building code run
// unchanged while all traffic is directed to the in-process test server.
func redirectingClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	srvHost := strings.TrimPrefix(srv.URL, "http://")
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			r2 := r.Clone(r.Context())
			r2.URL.Scheme = "http"
			r2.URL.Host = srvHost
			return http.DefaultTransport.RoundTrip(r2)
		}),
	}
}

// newTestUpdater constructs an Updater backed by srv with no-op replaceFn and
// restartFn so tests never touch real binaries.
func newTestUpdater(t *testing.T, srv *httptest.Server) *Updater {
	t.Helper()
	u, err := New(Config{
		Owner:          "franc",
		Repo:           "nametag-cc",
		CurrentVersion: "1.0.0",
		HTTPClient:     redirectingClient(t, srv),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	u.replaceFn = func(src, dst string) error { return nil }
	u.restartFn = func(path string, args []string) error { return nil }
	return u
}

// ---- Tests ---------------------------------------------------------------

func TestNew_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing Owner", Config{Repo: "r", CurrentVersion: "1.0.0"}},
		{"missing Repo", Config{Owner: "o", CurrentVersion: "1.0.0"}},
		{"missing CurrentVersion", Config{Owner: "o", Repo: "r"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestFetchLatestRelease_Success(t *testing.T) {
	want := githubRelease{
		TagName: "v2.0.0",
		Assets: []githubAsset{
			{Name: "nametag-linux-amd64", BrowserDownloadURL: "http://example.com/nametag-linux-amd64"},
			{Name: "checksums.txt", BrowserDownloadURL: "http://example.com/checksums.txt"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	got, err := fetchLatestRelease(context.Background(), redirectingClient(t, srv), "franc", "nametag-cc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TagName != want.TagName {
		t.Errorf("TagName = %q, want %q", got.TagName, want.TagName)
	}
	if len(got.Assets) != len(want.Assets) {
		t.Errorf("len(Assets) = %d, want %d", len(got.Assets), len(want.Assets))
	}
}

func TestFetchLatestRelease_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchLatestRelease(context.Background(), redirectingClient(t, srv), "franc", "nametag-cc")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestParseChecksums(t *testing.T) {
	content := `
# this is a comment
abc123  nametag-linux-amd64
DEF456  nametag-darwin-arm64

# another comment
789abc  nametag-windows-amd64.exe
`
	m := parseChecksums(content)

	cases := []struct{ name, want string }{
		{"nametag-linux-amd64", "abc123"},
		{"nametag-darwin-arm64", "def456"}, // DEF456 lowercased
		{"nametag-windows-amd64.exe", "789abc"},
	}
	for _, tc := range cases {
		got, ok := m[tc.name]
		if !ok {
			t.Errorf("key %q not found in map", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("m[%q] = %q, want %q", tc.name, got, tc.want)
		}
	}
	// Comment lines must not appear as keys.
	for k := range m {
		if strings.HasPrefix(k, "#") {
			t.Errorf("comment line ended up as key: %q", k)
		}
	}
}

func TestDownloadToTemp_Success(t *testing.T) {
	body := []byte("hello binary content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	u := &Updater{client: redirectingClient(t, srv)}
	path, hex, err := u.downloadToTemp(context.Background(), srv.URL+"/asset")
	if err != nil {
		t.Fatalf("downloadToTemp: %v", err)
	}
	if hex == "" {
		t.Error("expected non-empty SHA-256 hex")
	}
	t.Cleanup(func() { os.Remove(path) })

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading temp file: %v", err)
	}
	if string(data) != string(body) {
		t.Errorf("file content = %q, want %q", data, body)
	}
}

func TestDownloadToTemp_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	u := &Updater{client: redirectingClient(t, srv)}
	_, _, err := u.downloadToTemp(context.Background(), srv.URL+"/asset")
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}
}

func TestCheckAndUpdate_NoUpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.0.0"})
	}))
	defer srv.Close()

	u := newTestUpdater(t, srv)
	u.current = "1.0.0"
	replaceCalled := false
	u.replaceFn = func(src, dst string) error { replaceCalled = true; return nil }

	if err := u.CheckAndUpdate(context.Background()); err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if replaceCalled {
		t.Error("replaceFn should NOT have been called when no update is available")
	}
}

func TestCheckAndUpdate_UpdateApplied(t *testing.T) {
	assetName := (&Updater{}).platformAssetName()
	binaryContent := []byte("fake new binary")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "releases/latest"):
			release := githubRelease{
				TagName: "v2.0.0",
				Assets: []githubAsset{
					{Name: assetName, BrowserDownloadURL: "http://placeholder/asset"},
				},
			}
			json.NewEncoder(w).Encode(release)
		case strings.Contains(r.URL.Path, "asset"):
			w.Write(binaryContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u := newTestUpdater(t, srv)
	u.current = "1.0.0"

	replaceCalled := false
	restartCalled := false
	u.replaceFn = func(src, dst string) error { replaceCalled = true; return nil }
	u.restartFn = func(path string, args []string) error { restartCalled = true; return nil }

	if err := u.CheckAndUpdate(context.Background()); err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if !replaceCalled {
		t.Error("replaceFn should have been called")
	}
	if !restartCalled {
		t.Error("restartFn should have been called")
	}
}

func TestCheckAndUpdate_DevBuildSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v1.0.0"})
	}))
	defer srv.Close()

	u := newTestUpdater(t, srv)
	u.current = "dev"
	replaceCalled := false
	u.replaceFn = func(src, dst string) error { replaceCalled = true; return nil }

	if err := u.CheckAndUpdate(context.Background()); err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if replaceCalled {
		t.Error("replaceFn should NOT be called for dev builds")
	}
}

func TestPlatformAssetName(t *testing.T) {
	u := &Updater{}
	name := u.platformAssetName()
	if name == "" {
		t.Fatal("platformAssetName returned empty string")
	}
	if !strings.HasPrefix(name, "nametag-") {
		t.Errorf("platformAssetName = %q; want prefix %q", name, "nametag-")
	}
}
