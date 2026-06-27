package version_test

import (
	"strings"
	"testing"

	"github.com/franc/nametag-cc/internal/version"
)

func TestString(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	version.Version = "1.2.3"
	got := version.String()

	if !strings.Contains(got, version.AppName) {
		t.Errorf("String() = %q, want it to contain app name %q", got, version.AppName)
	}
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("String() = %q, want it to contain version %q", got, "1.2.3")
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		candidate string
		want      bool
		wantErr   bool
	}{
		{name: "newer patch", current: "1.0.0", candidate: "1.0.1", want: true},
		{name: "newer minor", current: "1.0.0", candidate: "1.1.0", want: true},
		{name: "newer major", current: "1.0.0", candidate: "2.0.0", want: true},
		{name: "same version", current: "1.0.0", candidate: "1.0.0", want: false},
		{name: "older", current: "1.2.0", candidate: "1.1.9", want: false},
		{name: "v-prefix on current", current: "v1.0.0", candidate: "1.0.1", want: true},
		{name: "v-prefix on candidate", current: "1.0.0", candidate: "v1.0.1", want: true},
		{name: "dev build", current: "dev", candidate: "1.0.0", want: false, wantErr: false},
		{name: "invalid current", current: "not-semver", candidate: "1.0.0", wantErr: true},
		{name: "invalid candidate", current: "1.0.0", candidate: "not-semver", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := version.IsNewer(tc.current, tc.candidate)

			if (err != nil) != tc.wantErr {
				t.Fatalf("IsNewer(%q, %q) error = %v, wantErr %v", tc.current, tc.candidate, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
			}
		})
	}
}
