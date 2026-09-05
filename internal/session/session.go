// Package session manages the lifecycle of one external activity on the
// Roam map: an initial set, periodic heartbeats that keep it alive, an
// optional local timer that ends it automatically, and an explicit clear.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nathabonfim59/roamming/internal/roam"
)

const (
	// openTTL is the TTL posted for open-ended activities. Longer TTLs
	// mean fewer posts; the trade-off is a badge that can linger up to
	// openTTL if the app dies without clearing.
	openTTL = 30 * time.Minute
	// heartbeatEvery keeps open-ended activities alive well within their
	// TTL.
	heartbeatEvery = 15 * time.Minute
	// maxTTL is the server-side cap; larger values are clamped anyway.
	maxTTL = time.Hour
	// countdownEvery refreshes live-countdown badges so the map text
	// ticks "back in Xm" every minute.
	countdownEvery = time.Minute
	// longTimerEvery heartbeats timers longer than maxTTL often enough
	// that each post's TTL (remaining + grace, clamped) never lapses.
	longTimerEvery = 45 * time.Minute
	// grace keeps a timed row alive a few seconds past its end so the
	// final clear always lands on a live row (and covers clock skew).
	grace = 30 * time.Second
	// clearTimeout bounds the best-effort clear calls.
	clearTimeout = 10 * time.Second
)

// CountdownStyle picks how a timed activity presents its end.
type CountdownStyle int

const (
	// FixedEnd shows "— back at 15:04 BRT", computed once at start.
	// A timer within the server's 1-hour TTL posts exactly once.
	FixedEnd CountdownStyle = iota
	// LiveCountdown shows "— back in 24m", re-posted every minute so the
	// badge text updates on the Roam map.
	LiveCountdown
)

// Phase describes where the activity lifecycle currently stands.
type Phase int

const (
	PhaseIdle Phase = iota
	PhaseStarting
	PhaseRunning
	PhaseStopping
)

func (p Phase) String() string {
	switch p {
	case PhaseStarting:
		return "starting"
	case PhaseRunning:
		return "running"
	case PhaseStopping:
		return "stopping"
	default:
		return "idle"
	}
}

// Config is the user-chosen shape of the activity.
type Config struct {
	Display roam.Display
	DND     bool
	// Timer is how long the activity should last. Zero means "until I
	// stop it" (kept alive by heartbeats).
	Timer time.Duration
	// Style selects fixed "back at HH:MM" text vs a live countdown.
	Style CountdownStyle
}

// Validate applies the same rules as the API before we call it.
func (c Config) Validate() error {
	if c.Timer < 0 {
		return errors.New("timer cannot be negative")
	}
	return c.Display.Validate()
}

// State is a snapshot of the lifecycle, pushed to the UI on every change.
type State struct {
	Phase           Phase
	Display         roam.Display
	DND             bool
	StartedAt       time.Time
	ServerExpiresAt time.Time // server-stamped TTL expiry of the latest post
	EndsAt          time.Time // local timer end; zero when no timer
	LastError       error
}

// Manager runs at most one activity at a time.
type Manager struct {
	cl     *roam.Client
	userID string

	mu         sync.Mutex
	cancel     context.CancelFunc
	externalID string
	state      State
	onState    func(State)
}

// New returns a manager acting on the given (personal) user.
func New(cl *roam.Client, userID string) *Manager {
	return &Manager{cl: cl, userID: userID}
}

// OnState registers the callback invoked on every state change. It fires
// from background goroutines; UI callers must marshal back to the UI
// thread themselves (e.g. fyne.Do).
func (m *Manager) OnState(fn func(State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onState = fn
}

// State returns the current snapshot.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Start begins a new activity asynchronously. The first set happens in a
// goroutine; progress arrives through the OnState callback. It returns an
// error only for immediate rejection (already running, invalid config).
func (m *Manager) Start(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ext, err := newExternalID()
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return errors.New("an activity is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.externalID = ext

	startedAt := time.Now().UTC()
	var endsAt time.Time
	if cfg.Timer > 0 {
		endsAt = startedAt.Add(cfg.Timer)
	}
	m.state = State{
		Phase:     PhaseStarting,
		Display:   cfg.Display,
		DND:       cfg.DND,
		StartedAt: startedAt,
		EndsAt:    endsAt,
	}
	cb := m.onState
	snap := m.state
	m.mu.Unlock()
	notify(cb, snap)

	go m.run(ctx, cfg, ext, endsAt, cb)
	return nil
}

// run posts the initial activity, then keeps it alive until the timer
// ends or the manager is stopped. How often it re-posts depends on the
// countdown style and timer length — see beatInterval.
func (m *Manager) run(ctx context.Context, cfg Config, ext string, endsAt time.Time, cb func(State)) {
	act, err := m.post(ctx, cfg, ext, endsAt)
	if ctx.Err() != nil {
		return // Stop() owns the state now
	}
	if err != nil {
		m.fail(err, cb)
		return
	}
	m.update(func(s *State) {
		s.Phase = PhaseRunning
		s.Display = act.Display
		s.ServerExpiresAt = act.ExpiresAt
	}, cb)

	var tickC <-chan time.Time // nil = no periodic re-posts
	if interval := beatInterval(cfg); interval > 0 {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		tickC = tick.C
	}
	var endCh <-chan time.Time
	if !endsAt.IsZero() {
		timer := time.NewTimer(time.Until(endsAt))
		defer timer.Stop()
		endCh = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-endCh:
			m.finish(ext, cb)
			return
		case <-tickC:
			ttl := postTTL(cfg, endsAt)
			if ttl <= 0 {
				m.finish(ext, cb)
				return
			}
			act, err := m.post(ctx, cfg, ext, endsAt)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if errors.Is(err, roam.ErrNotAuthenticated) {
					// The grant is gone (revoked/expired); retrying is pointless.
					m.fail(err, cb)
					return
				}
				// Transient failures keep the previous post alive until its
				// TTL lapses; surface the error and retry on the next tick.
				m.update(func(s *State) { s.LastError = err }, cb)
				continue
			}
			m.update(func(s *State) {
				s.Phase = PhaseRunning
				s.Display = act.Display
				s.ServerExpiresAt = act.ExpiresAt
				s.LastError = nil
			}, cb)
		}
	}
}

// post sends one set (create or heartbeat) for ext. The display text is
// recomputed per post: identical every time for FixedEnd, freshly
// counted down for LiveCountdown. Callers must ensure the timer has not
// ended (positive postTTL).
func (m *Manager) post(ctx context.Context, cfg Config, ext string, endsAt time.Time) (*roam.Activity, error) {
	return m.cl.SetActivity(ctx, roam.SetActivityRequest{
		UserID:     m.userID,
		ExternalID: ext,
		Display:    displayFor(cfg, endsAt),
		TTLSeconds: int(postTTL(cfg, endsAt) / time.Second),
		DND:        cfg.DND,
	})
}

// displayFor renders the display for the current moment. Timed
// activities get a suffix on the title (within the API's 140-code-point
// limit): a fixed "— back at 15:04 BRT" in the user's timezone, or a
// live "— back in 24m" for the countdown style. Open-ended activities
// keep the title untouched.
func displayFor(cfg Config, endsAt time.Time) roam.Display {
	d := cfg.Display
	if cfg.Timer <= 0 || endsAt.IsZero() {
		return d
	}
	var suffix string
	if cfg.Style == LiveCountdown {
		remaining := time.Until(endsAt)
		if remaining <= 0 {
			return d
		}
		suffix = " — back in " + fmtDuration(remaining)
	} else {
		suffix = " — back at " + endsAt.Local().Format("15:04 MST")
	}
	if utf8.RuneCountInString(d.Title)+utf8.RuneCountInString(suffix) <= 140 {
		d.Title += suffix
	}
	return d
}

// fmtDuration renders "45s", "12m", "1h05m" style durations.
func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	if d < time.Minute {
		return "<1m"
	}
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int(d/time.Minute) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// Stop cancels the loop and clears the activity with a fresh context.
func (m *Manager) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), clearTimeout)
	defer cancel()
	m.StopCtx(ctx)
}

// StopCtx is Stop with a caller-provided deadline for the clear call.
func (m *Manager) StopCtx(ctx context.Context) {
	m.mu.Lock()
	cancel, ext := m.cancel, m.externalID
	running := cancel != nil
	if running {
		m.cancel = nil
		m.state.Phase = PhaseStopping
	}
	cb, snap := m.onState, m.state
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}

	if !running {
		return
	}
	cancel()
	_ = m.cl.ClearActivity(ctx, m.userID, ext)

	m.mu.Lock()
	m.cancel = nil
	m.state = State{}
	cb, snap = m.onState, m.state
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}
}

// finish ends the activity when the timer ran out inside the loop.
func (m *Manager) finish(ext string, cb func(State)) {
	ctx, cancel := context.WithTimeout(context.Background(), clearTimeout)
	defer cancel()
	_ = m.cl.ClearActivity(ctx, m.userID, ext)
	m.mu.Lock()
	m.cancel = nil
	m.state = State{}
	cb, snap := m.onState, m.state
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}
}

// fail records an error and drops back to idle.
func (m *Manager) fail(err error, cb func(State)) {
	m.mu.Lock()
	m.cancel = nil
	m.state = State{LastError: err}
	cb, snap := m.onState, m.state
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}
}

// update mutates the state and notifies.
func (m *Manager) update(f func(*State), cb func(State)) {
	m.mu.Lock()
	f(&m.state)
	snap := m.state
	m.mu.Unlock()
	if cb != nil {
		cb(snap)
	}
}

// postTTL picks ttlSeconds for the next post: a fixed 30 minutes when
// running open-ended, otherwise the time left on the timer plus a small
// grace (clamped to the server's 1-hour maximum).
func postTTL(cfg Config, endsAt time.Time) time.Duration {
	if cfg.Timer <= 0 {
		return openTTL
	}
	remaining := time.Until(endsAt)
	if remaining <= 0 {
		return 0
	}
	return min(remaining+grace, maxTTL)
}

// beatInterval picks how often the activity is re-posted:
//
//   - open-ended: every 15 min within a 30-min TTL (few posts)
//   - live countdown: every minute, to refresh the badge text
//   - fixed end with timer <= 1 h: never — the single post's TTL already
//     covers the end (plus grace), so nothing re-posts
//   - fixed end with timer > 1 h: every 45 min, within the 1-hour TTL cap
//
// Zero means "no periodic re-posts".
func beatInterval(cfg Config) time.Duration {
	switch {
	case cfg.Timer <= 0:
		return heartbeatEvery
	case cfg.Style == LiveCountdown:
		return countdownEvery
	case cfg.Timer <= maxTTL:
		return 0
	default:
		return longTimerEvery
	}
}

func notify(cb func(State), s State) {
	if cb != nil {
		cb(s)
	}
}

// newExternalID builds a session key unique per integration and user,
// prefixed with the product name as the API recommends.
func newExternalID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate external id: %w", err)
	}
	return "roamming:session:" + hex.EncodeToString(b[:]), nil
}
