package roam

import (
	"errors"
	"strings"
	"testing"
)

func TestCredentialsRoundtrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := LoadCredentials(); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("empty store: err = %v, want ErrNotAuthenticated", err)
	}

	in := &Credentials{
		ClientID:     "cid",
		RefreshToken: "rt",
		AccessToken:  "at",
		UserID:       "uid",
		UserName:     "Jane",
		RoamName:     "Acme",
		Scopes:       []string{ScopeWriteActivity, ScopeReadActivity},
	}
	path, err := SaveCredentials(in)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	t.Log("saved to", path)

	out, err := LoadCredentials()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.AccessToken != "at" || out.UserID != "uid" || out.UserName != "Jane" || out.RoamName != "Acme" {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
	if !out.HasScope(ScopeWriteActivity) || out.HasScope("chat:send_message") {
		t.Fatalf("scope check broken: %v", out.Scopes)
	}

	if err := ClearCredentials(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := ClearCredentials(); err != nil { // idempotent
		t.Fatalf("second clear: %v", err)
	}
	if _, err := LoadCredentials(); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("after clear: err = %v, want ErrNotAuthenticated", err)
	}
}

func TestDisplayValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		display Display
		wantErr bool
	}{
		{"ok minimal", Display{Emoji: "📞", Title: "On a call"}, false},
		{"ok full", Display{Emoji: "📞", Title: "T", Subtitle: "S", Color: "green"}, false},
		{"missing emoji", Display{Title: "T"}, true},
		{"missing title", Display{Emoji: "📞"}, true},
		{"bad color", Display{Emoji: "📞", Title: "T", Color: "#00FF00"}, true},
		{"agent glow name", Display{Emoji: "🤖", Title: "T", Color: "claude"}, true},
	}
	for _, tc := range cases {
		err := tc.display.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}

	// Limits count code points, not bytes: 16 emoji (multi-byte) pass,
	// 17 fail; a 140-rune CJK title passes.
	long := Display{Emoji: strings.Repeat("🎯", 17), Title: "T"}
	if err := long.Validate(); err == nil {
		t.Error("17-code-point emoji should be rejected")
	}
	ok := Display{Emoji: strings.Repeat("🎯", 16), Title: strings.Repeat("状", 140)}
	if err := ok.Validate(); err != nil {
		t.Errorf("limit-respecting display rejected: %v", err)
	}
}
