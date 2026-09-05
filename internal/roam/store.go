package roam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrNotAuthenticated is returned when no usable grant is stored locally.
var ErrNotAuthenticated = errors.New("not authenticated with Roam")

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

func credentialsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "roamming"), nil
}

// SaveCredentials persists the grant with owner-only file permissions.
func SaveCredentials(c *Credentials) (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode credentials: %w", err)
	}
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write credentials: %w", err)
	}
	return path, nil
}

// LoadCredentials reads the stored grant. It returns ErrNotAuthenticated
// when no credentials have been saved yet.
func LoadCredentials() (*Credentials, error) {
	dir, err := credentialsDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotAuthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if !c.Connected() {
		return nil, ErrNotAuthenticated
	}
	return &c, nil
}

// ClearCredentials removes the stored grant, if any.
func ClearCredentials() error {
	dir, err := credentialsDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "credentials.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove credentials: %w", err)
	}
	return nil
}
