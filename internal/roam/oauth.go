package roam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuth endpoints from https://developer.ro.am/docs/guides/oauth.
const (
	AuthorizeURL = "https://ro.am/oauth/authorize"
	TokenURL     = "https://ro.am/oauth/token"
)

// ScopeWriteActivity / ScopeReadActivity are the only scopes this app
// requests. They cover the external user activity API and nothing else.
const (
	ScopeWriteActivity = "user:write.activity"
	ScopeReadActivity  = "user:read.activity"
)

// RequestScopes is the space-separated scope list sent on the authorize URL.
const RequestScopes = ScopeWriteActivity + " " + ScopeReadActivity

// DefaultRedirectURI must be registered on the OAuth app exactly as-is
// (Roam does exact-match redirect URIs; localhost is allowed for local
// apps). The temporary callback server below listens on this address.
const DefaultRedirectURI = "http://127.0.0.1:18079/callback"

// OAuthConfig carries the registered client settings for one flow.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string // empty for a public (PKCE) client
	RedirectURI  string
	Timeout      time.Duration // whole-flow deadline; 0 defaults to 5m
}

// Auth manages the OAuth grant: the browser flow with a short-lived
// localhost callback server, token exchange/refresh, and revocation.
type Auth struct {
	mu     sync.Mutex
	creds  *Credentials
	onLost func(reason string)
	http   *http.Client
}

// NewAuth loads any previously stored grant.
func NewAuth() *Auth {
	creds, _ := LoadCredentials()
	return &Auth{creds: creds, http: &http.Client{Timeout: 30 * time.Second}}
}

// OnGrantLost registers a callback fired (on its own goroutine) when the
// grant becomes unusable, e.g. the user revoked it in Roam.
func (a *Auth) OnGrantLost(fn func(reason string)) { a.onLost = fn }

// Connected reports whether a usable grant is currently held.
func (a *Auth) Connected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.creds.Connected()
}

// Identity returns the stored identity, if any.
func (a *Auth) Identity() (userID, userName, roamName string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.creds == nil {
		return "", "", ""
	}
	return a.creds.UserID, a.creds.UserName, a.creds.RoamName
}

// Creds returns a snapshot of the stored grant.
func (a *Auth) Creds() (Credentials, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.creds == nil {
		return Credentials{}, false
	}
	return *a.creds, true
}

// SetIdentity records the token.info identity on the stored grant.
func (a *Auth) SetIdentity(info *TokenInfo) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.creds == nil {
		return ErrNotAuthenticated
	}
	a.creds.UserID = info.User.ID
	a.creds.UserName = info.User.Name
	a.creds.RoamName = info.Roam.Name
	a.creds.Scopes = info.Scopes
	return SaveCredentials(a.creds)
}

// Access returns a valid bearer token, refreshing it first when it is
// about to expire.
func (a *Auth) Access(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.creds.Connected() {
		return "", ErrNotAuthenticated
	}
	if time.Until(a.creds.ExpiresAt) > 30*time.Second {
		return a.creds.AccessToken, nil
	}
	if err := a.refreshLocked(ctx); err != nil {
		return "", err
	}
	return a.creds.AccessToken, nil
}

// Refresh forces a token refresh (used after a 401 invalid_token).
func (a *Auth) Refresh(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refreshLocked(ctx)
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in,omitzero"`
	Scope            string `json:"scope,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (a *Auth) refreshLocked(ctx context.Context) error {
	c := a.creds
	if c == nil || c.RefreshToken == "" {
		a.grantLostLocked("No refresh token; sign in again")
		return ErrNotAuthenticated
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.RefreshToken},
		"client_id":     {c.ClientID},
	}
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	var resp tokenResponse
	if err := a.postForm(ctx, TokenURL, form, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		if resp.Error == "invalid_grant" {
			a.grantLostLocked("Your Roam authorization expired; sign in again")
			return fmt.Errorf("refresh rejected: %s", resp.ErrorDescription)
		}
		return fmt.Errorf("refresh failed: %s (%s)", resp.Error, resp.ErrorDescription)
	}
	c.AccessToken = resp.AccessToken
	// Refresh rotation: discard the old refresh token when a new one arrives.
	if resp.RefreshToken != "" {
		c.RefreshToken = resp.RefreshToken
	}
	if resp.ExpiresIn > 0 {
		c.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	return SaveCredentials(c)
}

// GrantLost drops the local grant and notifies the callback.
func (a *Auth) GrantLost(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grantLostLocked(reason)
}

// grantLostLocked must be called with a.mu held.
func (a *Auth) grantLostLocked(reason string) {
	if !a.creds.Connected() && a.creds == nil {
		return
	}
	a.creds = nil
	_ = ClearCredentials()
	if a.onLost != nil {
		cb := a.onLost
		go cb(reason)
	}
}

// Disconnect revokes the grant in Roam (best effort) and deletes the
// locally stored tokens.
func (a *Auth) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	c := a.creds
	a.creds = nil
	a.mu.Unlock()
	_ = ClearCredentials()
	if c == nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/token.revoke", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// tokenInfo fetches identity and granted scopes for a fresh access
// token (GET /token.info needs no scope).
func (a *Auth) tokenInfo(ctx context.Context, accessToken string) (*TokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/token.info", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call token.info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token.info: unexpected status %s", resp.Status)
	}
	var info TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode token.info: %w", err)
	}
	return &info, nil
}

// postForm posts form-encoded data and decodes a JSON response.
func (a *Auth) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// OAuth endpoints report the reason in a JSON body even on
		// failure (e.g. 401 invalid_client); a bare status hides it.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		var rejected tokenResponse
		if json.Unmarshal(body, &rejected) == nil && rejected.Error != "" {
			return fmt.Errorf("%s: %s (%s)", endpoint, rejected.Error, rejected.ErrorDescription)
		}
		return fmt.Errorf("%s: unexpected status %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Connect runs the full authorization-code flow:
//
//  1. spin up a temporary stdlib net/http server on the localhost
//     redirect address,
//  2. open the authorize URL in the browser (via openBrowser),
//  3. capture the ?code= redirect,
//  4. exchange it for tokens (PKCE, plus the secret for confidential
//     clients),
//  5. resolve the identity via token.info and persist everything.
//
// It blocks until the flow completes, fails, or ctx is canceled.
func (a *Auth) Connect(ctx context.Context, cfg OAuthConfig, openBrowser func(*url.URL) error) (*Credentials, error) {
	if cfg.ClientID == "" {
		return nil, errors.New("a Roam OAuth Client ID is required")
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = DefaultRedirectURI
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	verifier, err := newCodeVerifier()
	if err != nil {
		return nil, err
	}
	state, err := randomToken()
	if err != nil {
		return nil, err
	}

	authorize := AuthorizeURL + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {cfg.RedirectURI},
		"scope":                 {RequestScopes},
		"state":                 {state},
		"code_challenge":        {s256Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}.Encode()

	cbs, err := startCallbackServer(cfg.RedirectURI, state)
	if err != nil {
		return nil, err
	}
	defer cbs.close()

	if openBrowser != nil {
		u, err := url.Parse(authorize)
		if err != nil {
			return nil, err
		}
		if err := openBrowser(u); err != nil {
			return nil, fmt.Errorf("open browser: %w (authorize URL: %s)", err, authorize)
		}
	}

	code, err := cbs.wait(ctx)
	if err != nil {
		return nil, err
	}

	creds, err := a.exchangeCode(ctx, cfg, code, verifier)
	if err != nil {
		return nil, err
	}

	info, err := a.tokenInfo(ctx, creds.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("fetch token info: %w", err)
	}
	creds.UserID = info.User.ID
	creds.UserName = info.User.Name
	creds.RoamName = info.Roam.Name
	creds.Scopes = info.Scopes

	a.mu.Lock()
	a.creds = creds
	saveErr := SaveCredentials(creds)
	a.mu.Unlock()
	if saveErr != nil {
		return nil, saveErr
	}
	return creds, nil
}

// exchangeCode swaps the authorization code for tokens at the token
// endpoint. Public clients authenticate with the PKCE verifier only;
// confidential clients additionally send their secret (client_secret_post).
func (a *Auth) exchangeCode(ctx context.Context, cfg OAuthConfig, code, verifier string) (*Credentials, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURI},
		"client_id":     {cfg.ClientID},
		"code_verifier": {verifier},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	var resp tokenResponse
	if err := a.postForm(ctx, TokenURL, form, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		err := fmt.Errorf("token exchange failed: %s (%s)", resp.Error, resp.ErrorDescription)
		// A confidential client registered without sending its secret
		// fails exactly here; point at the fix instead of the protocol.
		if resp.Error == "invalid_client" && cfg.ClientSecret == "" {
			err = fmt.Errorf("%w; this OAuth app was created with a client secret — paste it into the app's Client Secret field and connect again", err)
		}
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, errors.New("token endpoint returned no access token")
	}
	c := &Credentials{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURI:  cfg.RedirectURI,
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}
	return c, nil
}

// callbackServer is the temporary, stdlib-only loopback web server that
// receives the OAuth redirect. It serves exactly one purpose: capture
// the authorization code from GET /callback, validate the state
// parameter, show the user a close-this-tab page, and shut down.
type callbackServer struct {
	srv       *http.Server
	ln        net.Listener
	wantState string // expected OAuth state parameter
	result    chan callbackResult
}

type callbackResult struct {
	code string
	err  error
}

// startCallbackServer binds the loopback address from the redirect URI
// and begins serving. The server runs until close is called.
func startCallbackServer(redirect, state string) (*callbackServer, error) {
	addr, err := callbackListenAddr(redirect)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("open local callback server on %s (is roamming already running?): %w", addr, err)
	}
	cbs := &callbackServer{
		ln:        ln,
		wantState: state,
		result:    make(chan callbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", cbs.handle)
	cbs.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = cbs.srv.Serve(ln) }()
	return cbs, nil
}

// addr is the host:port the server is listening on.
func (c *callbackServer) addr() string { return c.ln.Addr().String() }

func (c *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code, errCode := q.Get("code"), q.Get("error")
	errDesc, gotState := q.Get("error_description"), q.Get("state")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	switch {
	case errCode != "":
		msg := errDesc
		if msg == "" {
			msg = errCode
		}
		fmt.Fprint(w, callbackPage(false, msg))
		c.result <- callbackResult{err: fmt.Errorf("authorization refused: %s (%s)", errCode, errDesc)}
	case gotState == "":
		fmt.Fprint(w, callbackPage(false, "missing state parameter; try again from the app."))
		c.result <- callbackResult{err: errors.New("authorization callback carried no state; try again")}
	case gotState != c.wantState:
		fmt.Fprint(w, callbackPage(false, "state check failed (possible CSRF); try again from the app."))
		c.result <- callbackResult{err: errors.New("authorization callback failed the state check (possible CSRF); try again")}
	case code == "":
		fmt.Fprint(w, callbackPage(false, "no authorization code received; try again from the app."))
		c.result <- callbackResult{err: errors.New("authorization callback carried no code; try again")}
	default:
		fmt.Fprint(w, callbackPage(true, ""))
		c.result <- callbackResult{code: code}
	}
}

// wait blocks until the redirect arrives or ctx is done.
func (c *callbackServer) wait(ctx context.Context) (string, error) {
	select {
	case r := <-c.result:
		return r.code, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("authorization timed out or was canceled: %w", ctx.Err())
	}
}

func (c *callbackServer) close() { _ = c.srv.Close() }

// callbackListenAddr extracts a loopback host:port from the redirect URI.
func callbackListenAddr(redirect string) (string, error) {
	u, err := url.Parse(redirect)
	if err != nil {
		return "", fmt.Errorf("invalid redirect URI %q: %w", redirect, err)
	}
	port := u.Port()
	if port == "" {
		return "", fmt.Errorf("redirect URI %q must include an explicit port", redirect)
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return "", fmt.Errorf("redirect host %q is not local; only localhost callbacks are supported", u.Hostname())
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

func newCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func callbackPage(ok bool, msg string) string {
	var body string
	if ok {
		body = `<h1>&#9989; Connected</h1><p>Roam Activity is authorized.<br>You can close this tab and return to the app.</p>`
	} else {
		body = `<h1>&#10060; Not connected</h1><p>` + html.EscapeString(msg) + `</p><p>You can close this tab and retry from the app.</p>`
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>Roam Activity</title></head>" +
		`<body style="font-family:system-ui,sans-serif;text-align:center;padding-top:4rem;background:#111;color:#eee">` +
		body + "</body></html>"
}
