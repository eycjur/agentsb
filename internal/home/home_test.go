package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenIsNewer(t *testing.T) {
	newer := []byte(`{"claudeAiOauth":{"accessToken":"a","expiresAt":1788601765914}}`)
	older := []byte(`{"claudeAiOauth":{"accessToken":"b","expiresAt":1788601765913}}`)
	noToken := []byte(`{"claudeAiOauth":{"accessToken":"c"}}`)
	broken := []byte(`not json`)

	cases := []struct {
		name      string
		container []byte
		host      []byte
		want      bool
	}{
		{"container newer", newer, older, true},
		{"container older", older, newer, false},
		{"same expiresAt", newer, newer, false},
		{"host missing", newer, nil, true},
		{"host has no expiresAt", newer, noToken, true},
		{"host broken json", newer, broken, true},
		{"container has no expiresAt", noToken, older, false},
		{"container broken json", broken, older, false},
		{"both missing expiresAt", noToken, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := tokenIsNewer(c.container, c.host)
			if got != c.want {
				t.Errorf("tokenIsNewer = %v (reason %q), want %v", got, reason, c.want)
			}
			if !got && reason == "" {
				t.Errorf("skip without reason")
			}
		})
	}
}

func TestFindHostCursorAuth(t *testing.T) {
	home := t.TempDir()
	darwinPath := filepath.Join(home, ".cursor", "auth.json")
	linuxPath := filepath.Join(home, ".config", "cursor", "auth.json")

	t.Run("none", func(t *testing.T) {
		got, ok, err := findHostCursorAuth(home)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("expected missing, got %s", got)
		}
		if got != darwinPath {
			t.Fatalf("firstMissing = %q, want %q", got, darwinPath)
		}
	})

	t.Run("linux only", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(linuxPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(linuxPath, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, ok, err := findHostCursorAuth(home)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got != linuxPath {
			t.Fatalf("got (%v, %q), want linux %q", ok, got, linuxPath)
		}
	})

	t.Run("darwin preferred", func(t *testing.T) {
		if err := os.MkdirAll(filepath.Dir(darwinPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(darwinPath, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, ok, err := findHostCursorAuth(home)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got != darwinPath {
			t.Fatalf("got (%v, %q), want darwin %q", ok, got, darwinPath)
		}
	})
}
