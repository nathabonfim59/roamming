package roam

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

func TestAuthRestoresCredentialsAfterRestart(t *testing.T) {
	keyring.MockInit()
	redirectConfigDir(t)

	if NewAuth().Connected() {
		t.Fatal("fresh install should be disconnected")
	}
	if err := SaveCredentials(&Credentials{
		ClientID: "cid", AccessToken: "at", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(time.Hour),
		UserID:    "uid", UserName: "Jane", RoamName: "Acme",
	}); err != nil {
		t.Fatal(err)
	}

	// Each new Auth represents a launch; quitting leaves the saved grant intact.
	for range 2 {
		auth := NewAuth()
		if !auth.Connected() {
			t.Fatal("restart lost the saved connection")
		}
		if token, err := auth.Access(t.Context()); err != nil || token != "at" {
			t.Fatalf("restored access token = %q, err = %v", token, err)
		}
		if id, name, roam := auth.Identity(); id != "uid" || name != "Jane" || roam != "Acme" {
			t.Fatalf("restored identity = %q, %q, %q", id, name, roam)
		}
		if creds, ok := auth.Creds(); !ok || creds.RefreshToken != "rt" || creds.ClientID != "cid" {
			t.Fatal("restart lost refresh credentials")
		}
	}
	if err := ClearCredentials(); err != nil {
		t.Fatal(err)
	}
	if NewAuth().Connected() {
		t.Fatal("cleared credentials should stay disconnected after restart")
	}
}

func TestCredentialsRoundtrip(t *testing.T) {
	keyring.MockInit()
	redirectConfigDir(t)

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
	if err := SaveCredentials(in); err != nil {
		t.Fatalf("save: %v", err)
	}

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

// redirectConfigDir points the store's legacy-location resolution at a
// fresh temp directory and restores the original on cleanup. Going
// through the seam (instead of XDG_CONFIG_HOME) keeps the tests honest
// on macOS, where os.UserConfigDir ignores that variable.
func redirectConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := userConfigDir
	userConfigDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userConfigDir = prev })
	return dir
}

// writeLegacyCredentials plants a credentials.json in the pre-keyring
// location and returns its path.
func writeLegacyCredentials(t *testing.T, c *Credentials) string {
	t.Helper()
	path := filepath.Join(redirectConfigDir(t), "roamming", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLegacyFileMigratesToKeychain(t *testing.T) {
	keyring.MockInit()
	path := writeLegacyCredentials(t, &Credentials{ClientID: "cid", AccessToken: "at", RefreshToken: "rt"})

	out, err := LoadCredentials()
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if out.AccessToken != "at" || out.RefreshToken != "rt" {
		t.Fatalf("legacy load mismatch: %+v", out)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file still on disk after migration: %v", err)
	}
	// The grant now lives in the keychain: it must load with the file gone.
	out, err = LoadCredentials()
	if err != nil {
		t.Fatalf("load from keychain: %v", err)
	}
	if out.AccessToken != "at" {
		t.Fatalf("keychain load mismatch: %+v", out)
	}
}

func TestLegacyFileKeptWhenKeychainUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service running"))
	path := writeLegacyCredentials(t, &Credentials{ClientID: "cid", AccessToken: "at"})

	// Without a keychain the grant must still be served, and the
	// plaintext file kept so the migration can be retried later.
	out, err := LoadCredentials()
	if err != nil {
		t.Fatalf("load legacy without keychain: %v", err)
	}
	if out.AccessToken != "at" {
		t.Fatalf("legacy load mismatch: %+v", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy file should have been kept: %v", err)
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
