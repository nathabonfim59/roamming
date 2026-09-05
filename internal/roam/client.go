// Package roam is a minimal client for the Roam API (https://developer.ro.am)
// covering exactly what this app needs: OAuth authentication and the
// external user activity endpoints (user.activity.set / .clear / .list).
//
// It uses the personal access model: the app can only ever act as the
// signed-in user, with the two activity scopes requested at consent.
package roam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const apiBase = "https://api.ro.am/v1"

// Colors is the curated glow palette the API accepts. Hex values are not
// part of the API; clients resolve names for rendering.
var Colors = []string{
	"blue", "gold", "gray", "green", "indigo", "lime",
	"orange", "pink", "purple", "red", "teal", "yellow",
}

// Display is what the Roam client renders: a seat badge, hover tooltip,
// and optional glow. Limits count Unicode code points, not bytes.
type Display struct {
	Emoji    string `json:"emoji"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Color    string `json:"color,omitempty"`
}

// Validate checks the field limits and palette names enforced by the API.
func (d Display) Validate() error {
	if n := utf8.RuneCountInString(d.Emoji); n == 0 {
		return errors.New("an emoji is required")
	} else if n > 16 {
		return fmt.Errorf("emoji is too long (%d/16 code points)", n)
	}
	if n := utf8.RuneCountInString(d.Title); n == 0 {
		return errors.New("a status text is required")
	} else if n > 140 {
		return fmt.Errorf("status text is too long (%d/140 code points)", n)
	}
	if n := utf8.RuneCountInString(d.Subtitle); n > 140 {
		return fmt.Errorf("subtitle is too long (%d/140 code points)", n)
	}
	if d.Color != "" && !validColor(d.Color) {
		return fmt.Errorf("%q is not a Roam palette color", d.Color)
	}
	return nil
}

func validColor(name string) bool {
	return slices.Contains(Colors, name)
}

// Activity is a live external activity row as returned by the API.
type Activity struct {
	UserID     string    `json:"userId"`
	ExternalID string    `json:"externalId"`
	Display    Display   `json:"display"`
	DND        bool      `json:"dnd,omitzero"`
	StartedAt  time.Time `json:"startedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// SetActivityRequest is the body of user.activity.set. Re-posting the
// same ExternalID upserts the row and acts as a heartbeat; leave
// StartedAt nil on heartbeats to preserve the original start time.
type SetActivityRequest struct {
	UserID     string     `json:"userId"`
	ExternalID string     `json:"externalId"`
	Display    Display    `json:"display"`
	TTLSeconds int        `json:"ttlSeconds,omitzero"`
	DND        bool       `json:"dnd,omitzero"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
}

// APIError is a structured error returned by the Roam API.
type APIError struct {
	Status  int
	Code    string
	Message string
	Needed  []string // set on 403 missing_scope
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("roam: %s (%s)", e.Message, e.Code)
	}
	return "roam: " + msg
}

// ScopeMissing reports whether the error is a missing-scope rejection for s.
func (e *APIError) ScopeMissing() bool { return e.Code == "missing_scope" }

type apiErrorBody struct {
	Error   string   `json:"error"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Detail  string   `json:"detail"`
	Needed  []string `json:"needed"`
}

func (b apiErrorBody) errCode() string {
	if b.Code != "" {
		return b.Code
	}
	return b.Error
}

func (b apiErrorBody) errMsg() string {
	switch {
	case b.Message != "":
		return b.Message
	case b.Detail != "":
		return b.Detail
	default:
		return b.errCode()
	}
}

// TokenInfo is the response of GET /token.info (no scope required).
type TokenInfo struct {
	User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	ClientID string   `json:"clientId"`
	Scopes   []string `json:"scopes"`
	Roam     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"roam"`
}

// Client talks to the Roam API with an Auth-provided bearer token,
// transparently refreshing once after a 401 invalid_token.
type Client struct {
	auth *Auth
	http *http.Client
}

// NewClient returns an API client bound to the given Auth.
func NewClient(auth *Auth) *Client {
	return &Client{auth: auth, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		payload = raw
	}

	for attempt := range 2 {
		token, err := c.auth.Access(ctx)
		if err != nil {
			return err
		}
		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload) // fresh reader on every attempt
		}
		req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reqBody)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("call %s: %w", path, err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			if out == nil || resp.StatusCode == http.StatusNoContent || resp.ContentLength == 0 {
				return nil
			}
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decode %s response: %w", path, err)
			}
			return nil
		}

		apiErr := decodeAPIError(resp)
		resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			switch apiErr.Code {
			case "token_revoked":
				c.auth.GrantLost("Your Roam authorization was revoked; connect again")
				return apiErr
			case "invalid_token":
				if attempt == 0 {
					if err := c.auth.Refresh(ctx); err == nil {
						continue // retry once with the fresh token
					}
				}
			}
		}
		return apiErr
	}
	return errors.New("unreachable")
}

func decodeAPIError(resp *http.Response) *APIError {
	apiErr := &APIError{Status: resp.StatusCode}
	var body apiErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		apiErr.Code = body.errCode()
		apiErr.Message = body.errMsg()
		apiErr.Needed = body.Needed
	}
	if apiErr.Code == "" {
		apiErr.Code = strings.ToLower(strings.ReplaceAll(http.StatusText(resp.StatusCode), " ", "_"))
	}
	return apiErr
}

// TokenInfo fetches info about the current token, including the
// authenticated user ID (needed to target user.activity endpoints).
func (c *Client) TokenInfo(ctx context.Context) (*TokenInfo, error) {
	var info TokenInfo
	if err := c.do(ctx, http.MethodGet, "/token.info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SetActivity starts or heartbeats an external activity.
func (c *Client) SetActivity(ctx context.Context, r SetActivityRequest) (*Activity, error) {
	var act Activity
	if err := c.do(ctx, http.MethodPost, "/user.activity.set", r, &act); err != nil {
		return nil, err
	}
	return &act, nil
}

// ClearActivity ends an external activity. The API answers 204 even when
// the row was already cleared or expired.
func (c *Client) ClearActivity(ctx context.Context, userID, externalID string) error {
	return c.do(ctx, http.MethodPost, "/user.activity.clear", map[string]string{
		"userId":     userID,
		"externalId": externalID,
	}, nil)
}

// ListActivities returns every live external activity row for the user,
// including rows owned by other integrations.
func (c *Client) ListActivities(ctx context.Context, userID string) ([]Activity, error) {
	var out struct {
		Activities []Activity `json:"activities"`
	}
	path := "/user.activity.list?userId=" + url.QueryEscape(userID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Activities, nil
}
