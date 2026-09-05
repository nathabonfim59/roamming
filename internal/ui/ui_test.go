package ui

import (
	"testing"
	"time"
)

func TestFmtRemaining(t *testing.T) {
	t.Parallel()
	cases := map[time.Duration]string{
		0:                "0m",
		-time.Minute:     "0m",
		30 * time.Second: "<1m",
		time.Minute:      "1m",
		90 * time.Second: "2m", // rounds
		24 * time.Minute: "24m",
		time.Hour:        "1h00m",
		90 * time.Minute: "1h30m",
		8 * time.Hour:    "8h00m",
	}
	for in, want := range cases {
		if got := fmtRemaining(in); got != want {
			t.Errorf("fmtRemaining(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestTimerOptions(t *testing.T) {
	t.Parallel()
	if timerOptions[0].d != 0 {
		t.Fatal("first timer option must be the open-ended one")
	}
	for i, o := range timerOptions {
		if o.label == "" {
			t.Errorf("option %d has no label", i)
		}
		if i > 0 && o.d <= timerOptions[i-1].d {
			t.Errorf("option %d (%v) breaks ascending order", i, o.d)
		}
	}
}

func TestPresetLabels(t *testing.T) {
	t.Parallel()
	labels := presetLabels()
	if len(labels) != len(presets) {
		t.Fatalf("labels = %d, presets = %d", len(labels), len(presets))
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if seen[l] {
			t.Errorf("duplicate preset label %q", l)
		}
		seen[l] = true
	}
}
