package home

import "testing"

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
