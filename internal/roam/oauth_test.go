package roam

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallbackServerSuccess(t *testing.T) {
	t.Parallel()
	cbs, err := startCallbackServer("http://127.0.0.1:18081/callback", "state123")
	if err != nil {
		t.Fatalf("start callback server: %v", err)
	}
	defer cbs.close()

	// Simulate the provider redirect in parallel with the wait. The
	// goroutine reports through a buffered channel instead of calling
	// t.Errorf directly: a *testing.T must not be used from a helper
	// goroutine after the test may have finished (races on slower
	// runners, e.g. Windows CI).
	errc := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + cbs.addr() + "/callback?code=abc123&state=state123")
		if err != nil {
			errc <- err
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Connected") {
			errc <- fmt.Errorf("success page missing: %s", body)
			return
		}
		errc <- nil
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	code, err := cbs.wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code != "abc123" {
		t.Fatalf("code = %q, want %q", code, "abc123")
	}
	if err := <-errc; err != nil {
		t.Errorf("simulated redirect: %v", err)
	}
}

func TestCallbackServerStateMismatch(t *testing.T) {
	t.Parallel()
	cbs, err := startCallbackServer("http://127.0.0.1:18082/callback", "expected")
	if err != nil {
		t.Fatalf("start callback server: %v", err)
	}
	defer cbs.close()

	go func() {
		resp, _ := http.Get("http://" + cbs.addr() + "/callback?code=x&state=forged")
		if resp != nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err := cbs.wait(ctx); err == nil {
		t.Fatal("expected state mismatch error, got nil")
	}
}

func TestCallbackServerProviderError(t *testing.T) {
	t.Parallel()
	cbs, err := startCallbackServer("http://127.0.0.1:18083/callback", "s")
	if err != nil {
		t.Fatalf("start callback server: %v", err)
	}
	defer cbs.close()

	go func() {
		resp, _ := http.Get("http://" + cbs.addr() + "/callback?error=access_denied&error_description=user+said+no&state=s")
		if resp != nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = cbs.wait(ctx)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v, want access_denied refusal", err)
	}
}

func TestCallbackListenAddr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		uri string
		ok  bool
	}{
		{"http://127.0.0.1:18079/callback", true},
		{"http://localhost:8765/callback", true},
		{"https://evil.example.com/callback", false},
		{"http://127.0.0.1/callback", false}, // no port
	}
	for _, tc := range cases {
		_, err := callbackListenAddr(tc.uri)
		if (err == nil) != tc.ok {
			t.Errorf("callbackListenAddr(%q) error = %v, want error: %v", tc.uri, err, !tc.ok)
		}
	}
}

// TestS256Challenge uses the RFC 7636 appendix B test vector.
func TestS256Challenge(t *testing.T) {
	t.Parallel()
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)
	if got := s256Challenge(verifier); got != challenge {
		t.Fatalf("challenge = %q, want %q", got, challenge)
	}
}

func TestNewCodeVerifier(t *testing.T) {
	t.Parallel()
	v, err := newCodeVerifier()
	if err != nil {
		t.Fatal(err)
	}
	// base64url of 32 bytes is always 43 chars of unreserved characters.
	if len(v) != 43 || strings.ContainsAny(v, "+/= ") {
		t.Fatalf("verifier %q is not a valid PKCE code verifier", v)
	}
}
