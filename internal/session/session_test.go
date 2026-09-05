package session

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nathabonfim59/roamming/internal/roam"
)

func TestPostTTL(t *testing.T) {
	t.Parallel()
	openEnded := Config{}
	if got := postTTL(openEnded, time.Time{}); got != openTTL {
		t.Fatalf("open-ended ttl = %v, want %v", got, openTTL)
	}

	// Timer longer than the server maximum clamps to the max.
	long := Config{Timer: 3 * time.Hour}
	endsAt := time.Now().Add(long.Timer)
	if got := postTTL(long, endsAt); got != maxTTL {
		t.Fatalf("long timer ttl = %v, want %v", got, maxTTL)
	}

	// Timer inside the cap leaves a small grace margin.
	short := Config{Timer: 15 * time.Minute}
	endsAt = time.Now().Add(short.Timer)
	got := postTTL(short, endsAt)
	if got <= 15*time.Minute || got > 16*time.Minute {
		t.Fatalf("short timer ttl = %v, want ~15m30s", got)
	}

	// Expired timer reports zero so the loop clears instead of posting.
	if got := postTTL(short, time.Now().Add(-time.Minute)); got != 0 {
		t.Fatalf("expired timer ttl = %v, want 0", got)
	}
}

func TestNewExternalID(t *testing.T) {
	t.Parallel()
	a, err := newExternalID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newExternalID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("external IDs must be unique")
	}
	if !strings.HasPrefix(a, "roamming:session:") {
		t.Fatalf("external ID %q missing product prefix", a)
	}
	if len(a) > 128 {
		t.Fatalf("external ID %q exceeds the 128 code-point API limit", a)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	if err := (Config{Display: roam.Display{Emoji: "📞", Title: "T"}}).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := (Config{Timer: -time.Minute}).Validate(); err == nil {
		t.Fatal("negative timer accepted")
	}
	if err := (Config{Display: roam.Display{Emoji: "📞"}}).Validate(); err == nil {
		t.Fatal("missing title accepted")
	}
}

func TestDisplayFor(t *testing.T) {
	t.Parallel()

	// FixedEnd: title carries a fixed "back at HH:MM TZ" suffix.
	fixed := Config{
		Display: roam.Display{Emoji: "🎯", Title: "Focusing"},
		Timer:   30 * time.Minute,
		Style:   FixedEnd,
	}
	endsAt := time.Now().Add(24 * time.Minute)
	got := displayFor(fixed, endsAt)
	if !strings.Contains(got.Title, "back at ") {
		t.Fatalf("fixed title = %q, want a fixed back-at time", got.Title)
	}
	if strings.Contains(got.Title, "back in") {
		t.Fatalf("fixed title = %q, must not contain a live countdown", got.Title)
	}
	// The fixed text is stable: recomputing yields the same string.
	if again := displayFor(fixed, endsAt); again.Title != got.Title {
		t.Fatalf("fixed title drifted: %q vs %q", again.Title, got.Title)
	}

	// LiveCountdown: title carries a live "back in Xm" suffix.
	live := fixed
	live.Style = LiveCountdown
	got = displayFor(live, endsAt)
	if !strings.Contains(got.Title, "back in 24m") {
		t.Fatalf("live title = %q, want live countdown suffix", got.Title)
	}

	// Open-ended activity: title stays untouched.
	untimed := Config{Display: roam.Display{Emoji: "🎯", Title: "Focusing"}}
	if got := displayFor(untimed, time.Time{}); got.Title != "Focusing" {
		t.Fatalf("open-ended title = %q, want untouched", got.Title)
	}

	// A 140-code-point title leaves no room for the suffix and stays valid.
	long := Config{
		Display: roam.Display{Emoji: "🎯", Title: strings.Repeat("状", 140)},
		Timer:   time.Minute,
	}
	got = displayFor(long, time.Now().Add(time.Minute))
	if n := utf8.RuneCountInString(got.Title); n != 140 {
		t.Fatalf("long title = %d code points, want 140 (suffix skipped)", n)
	}
}

func TestBeatInterval(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  Config
		want time.Duration
	}{
		{"open-ended", Config{}, heartbeatEvery},
		{"live countdown", Config{Timer: time.Hour, Style: LiveCountdown}, countdownEvery},
		{"fixed, timer fits the TTL", Config{Timer: 30 * time.Minute, Style: FixedEnd}, 0},
		{"fixed, long timer", Config{Timer: 3 * time.Hour, Style: FixedEnd}, longTimerEvery},
	}
	for _, tc := range cases {
		if got := beatInterval(tc.cfg); got != tc.want {
			t.Errorf("%s: beatInterval = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	t.Parallel()
	cases := map[time.Duration]string{
		0:                "0m",
		-5 * time.Second: "0m",
		20 * time.Second: "<1m",
		time.Minute:      "1m",
		24 * time.Minute: "24m",
		time.Hour:        "1h00m",
		90 * time.Minute: "1h30m",
	}
	for in, want := range cases {
		if got := fmtDuration(in); got != want {
			t.Errorf("fmtDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPhaseString(t *testing.T) {
	t.Parallel()
	if PhaseIdle.String() != "idle" || PhaseRunning.String() != "running" {
		t.Fatal("phase names drifted")
	}
}
