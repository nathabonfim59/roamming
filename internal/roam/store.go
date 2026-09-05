package roam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// ErrNotAuthenticated is returned when no usable grant is stored locally.
var ErrNotAuthenticated = errors.New("not authenticated with Roam")

// The grant lives in the OS keychain as a single secret (macOS Keychain,
// Windows Credential Manager, or a freedesktop.org Secret Service such as
// gnome-keyring/KWallet). The service name matches the macOS bundle ID
// and LaunchAgent label; the account is fixed because the app holds
// exactly one grant: the token owner's.
const (
	keyringService = "com.nathabonfim59.roamming"
	keyringAccount = "roamming"
)

// Credentials is the locally persisted OAuth grant plus the identity it
// belongs to. Only the token owner's account is ever targeted: this app
// uses the personal access model with the two external-activity scopes.
type Credentials struct {
	// Client registration used for token refreshes (a public/PKCE client
	// leaves ClientSecret empty).
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`

	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`

	// Identity resolved from token.info after the initial exchange.
	UserID   string   `json:"user_id,omitempty"`
	UserName string   `json:"user_name,omitempty"`
	RoamName string   `json:"roam_name,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
}

// Connected reports whether a grant is stored.
func (c *Credentials) Connected() bool { return c != nil && c.AccessToken != "" }

// HasScope reports whether the granted scopes include s.
func (c *Credentials) HasScope(s string) bool {
	if c == nil {
		return false
	}
	for _, got := range c.Scopes {
		if got == s {
			return true
		}
	}
	return false
}

// legacyCredentialsPath is where grants were stored as a plaintext JSON
// file before the keychain move (roamming <= 1.1.3). It only exists so
// existing installs can be migrated.
func legacyCredentialsPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "roamming", "credentials.json"), nil
}

// SaveCredentials stores the grant in the OS keychain and removes any
// leftover plaintext file from the pre-keyring format.
func SaveCredentials(c *Credentials) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := keyring.Set(keyringService, keyringAccount, string(data)); err != nil {
		return fmt.Errorf("store credentials in keychain: %w", err)
	}
	if path, err := legacyCredentialsPath(); err == nil {
		_ = os.Remove(path)
	}
	return nil
}

// LoadCredentials reads the stored grant. It returns ErrNotAuthenticated
// when no credentials have been saved yet. A grant found in the legacy
// plaintext file is migrated into the keychain on the way through; the
// file is also the fallback when the keychain itself is unavailable.
func LoadCredentials() (*Credentials, error) {
	secret, err := keyring.Get(keyringService, keyringAccount)
	if err == nil {
		var c Credentials
		if err := json.Unmarshal([]byte(secret), &c); err != nil {
			return nil, fmt.Errorf("parse stored credentials: %w", err)
		}
		if !c.Connected() {
			return nil, ErrNotAuthenticated
		}
		return &c, nil
	}
	// No keychain secret (or no working keychain): fall back to the
	// pre-keyring plaintext file. When that is missing too, a genuine
	// keychain error is more informative than "not authenticated".
	creds, lerr := loadLegacyCredentials()
	switch {
	case lerr == nil:
		return creds, nil
	case errors.Is(err, keyring.ErrNotFound):
		return nil, lerr
	default:
		return nil, fmt.Errorf("read keychain: %w", err)
	}
}

// loadLegacyCredentials reads a pre-keyring credentials.json, migrates it
// into the keychain, and removes the plaintext file. When the keychain
// refuses the secret (e.g. no Secret Service on a minimal Linux desktop),
// the grant is still served and the file kept, so the app keeps working
// and the next launch can retry the migration.
func loadLegacyCredentials() (*Credentials, error) {
	path, err := legacyCredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotAuthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse legacy credentials: %w", err)
	}
	if !c.Connected() {
		return nil, ErrNotAuthenticated
	}
	if err := keyring.Set(keyringService, keyringAccount, string(data)); err != nil {
		return &c, nil // keep the file; retry the migration next launch
	}
	_ = os.Remove(path)
	return &c, nil
}

// ClearCredentials removes the stored grant, if any.
func ClearCredentials() error {
	if err := keyring.Delete(keyringService, keyringAccount); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keychain entry: %w", err)
	}
	if path, err := legacyCredentialsPath(); err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove legacy credentials: %w", err)
		}
	}
	return nil
}
