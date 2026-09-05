// Package ui builds the Fyne desktop UI: the main window (connection
// screen + activity editor) and the OS system tray integration.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/nathabonfim59/roamming/internal/roam"
	"github.com/nathabonfim59/roamming/internal/session"
)

// Preference keys (stored by Fyne per app ID).
const (
	prefClientID     = "oauth.clientID"
	prefClientSecret = "oauth.clientSecret"
	prefRedirect     = "oauth.redirectURI"
	prefLastConfig   = "activity.lastConfig"
)

// timerOption is one selectable duration; zero means "until I stop it".
type timerOption struct {
	label string
	d     time.Duration
}

var timerOptions = []timerOption{
	{"No timer (until I stop it)", 0},
	{"15 minutes", 15 * time.Minute},
	{"30 minutes", 30 * time.Minute},
	{"45 minutes", 45 * time.Minute},
	{"1 hour", time.Hour},
	{"1 hour 30", 90 * time.Minute},
	{"2 hours", 2 * time.Hour},
	{"3 hours", 3 * time.Hour},
	{"4 hours", 4 * time.Hour},
	{"8 hours", 8 * time.Hour},
}

// preset is a one-click status that pre-fills the form (still editable).
type preset struct {
	label string
	emoji string
	title string
	color string
	dnd   bool
}

var presets = []preset{
	{"On a call", "📞", "On a call", "green", true},
	{"In a meeting", "🎥", "In a meeting", "orange", true},
	{"Screen sharing", "🖥️", "Screen sharing", "blue", false},
	{"Focusing", "🎯", "Heads-down focus time", "indigo", false},
	{"Pair programming", "🤝", "Pair programming", "teal", false},
	{"Writing docs", "📝", "Writing docs", "blue", false},
	{"Lunch", "🍽️", "Out for lunch", "gold", false},
	{"Coffee break", "☕", "Coffee break", "gold", false},
	{"Commuting", "🚆", "Commuting", "lime", false},
	{"Away (AFK)", "💤", "Away from keyboard", "gray", false},
	{"Custom…", "", "", "", false},
}

func presetLabels() []string {
	labels := make([]string, len(presets))
	for i, p := range presets {
		labels[i] = p.label
	}
	return labels
}

// persistedConfig is the last-used activity shape, restored on startup so
// the tray toggle works before any window interaction.
type persistedConfig struct {
	Emoji     string `json:"emoji"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Color     string `json:"color"`
	DND       bool   `json:"dnd"`
	TimerMin  int    `json:"timerMinutes"`
	Countdown string `json:"countdown,omitempty"` // "fixed" (default) | "live"
}

// App wires the Fyne app, both screens, the Roam client and the tray.
// All widget access happens on the UI thread; background goroutines
// marshal back with fyne.Do.
type App struct {
	fa   fyne.App
	win  fyne.Window
	desk desktop.App // nil when the platform has no tray support
	auth *roam.Auth

	client *roam.Client     // set once connected (UI thread only)
	mgr    *session.Manager // set once connected (UI thread only)

	stack       *fyne.Container
	connectBox  *fyne.Container
	activityBox *fyne.Container

	// Tray references (updated in place).
	tray       *fyne.Menu
	trayStatus *fyne.MenuItem
	trayToggle *fyne.MenuItem

	// Connect screen widgets.
	clientIDEntry     *widget.Entry
	clientSecretEntry *widget.Entry
	redirectEntry     *widget.Entry
	connectBtn        *widget.Button
	waitActivity      *widget.Activity
	waitLabel         *widget.Label
	waitRow           *fyne.Container
	authURLEntry      *widget.Entry
	authURLRow        *fyne.Container

	// Activity screen widgets.
	accountLabel   *widget.Label
	presetSelect   *widget.Select
	emojiEntry     *widget.Entry
	titleEntry     *widget.Entry
	subtitleEntry  *widget.Entry
	colorRow       *fyne.Container
	dndCheck       *widget.Check
	timerSelect    *widget.Select
	styleSelect    *widget.Select
	stateLine      *widget.RichText
	countdownLabel *widget.Label
	startStopBtn   *widget.Button
	liveList       *fyne.Container

	colorNames   []string // "" (quiet) first, then roam.Colors
	colorButtons []*widget.Button
	colorName    string

	state    session.State
	trayText string     // last rendered tray texts (skip no-op refreshes)
	trayIcon trayStatus // last tray icon state
}

// New builds the whole UI (window + tray). Call Show, then fa.Run.
func New(fa fyne.App) *App {
	a := &App{fa: fa, auth: roam.NewAuth()}
	fa.Settings().SetTheme(roamThemeInstance) // Roam dark look, always
	fa.SetIcon(AppIcon())                     // window/taskbar icon
	a.win = fa.NewWindow("Roam Activity")
	a.win.Resize(fyne.NewSize(520, 720))
	a.win.CenterOnScreen()

	a.auth.OnGrantLost(a.onGrantLost)

	a.buildConnectScreen()
	a.buildActivityScreen()
	a.stack = container.NewStack(a.connectBox, a.activityBox)
	a.win.SetContent(container.NewScroll(a.stack))
	a.win.SetCloseIntercept(a.win.Hide) // tray app: closing hides, Quit exits

	a.setupTray()

	if creds, ok := a.auth.Creds(); ok && creds.Connected() {
		a.client = roam.NewClient(a.auth)
		a.ensureSession()
		a.showActivity()
	} else {
		a.showConnect("")
	}

	go a.tickLoop()
	return a
}

// Activate shows and focuses the main window. It is the action behind
// the tray menu and the response to a second launch of the app.
func (a *App) Activate() {
	a.win.Show()
	a.win.RequestFocus()
}

// Show displays the main window.
func (a *App) Show() { a.win.Show() }

// ---- screen switching --------------------------------------------------

func (a *App) showConnect(reason string) {
	if a.mgr != nil {
		m := a.mgr
		go m.Stop() // best-effort clear in the background
	}
	a.client = nil
	a.mgr = nil
	a.waitActivity.Stop()
	a.waitLabel.SetText(reason)
	a.connectBtn.Enable()
	a.waitRow.Hide()
	a.authURLRow.Hide()
	a.connectBox.Show()
	a.activityBox.Hide()
	a.stack.Refresh()
	a.refreshTray()
}

func (a *App) showActivity() {
	a.connectBox.Hide()
	a.activityBox.Show()
	a.stack.Refresh()
	a.refreshLive()
	a.refreshTray()
}

// onGrantLost fires (on a goroutine) when the grant is revoked or has
// become unrecoverable; back to the connect screen.
func (a *App) onGrantLost(reason string) {
	fyne.Do(func() {
		a.showConnect(reason)
		dialog.NewError(errors.New(reason), a.win)
	})
}

// ---- connect screen -----------------------------------------------------

func (a *App) buildConnectScreen() {
	prefs := a.fa.Preferences()

	a.clientIDEntry = widget.NewEntry()
	a.clientIDEntry.SetPlaceHolder("Client ID from Roam Administration -> Developer")
	a.clientIDEntry.SetText(prefs.String(prefClientID))

	a.clientSecretEntry = widget.NewPasswordEntry()
	a.clientSecretEntry.SetPlaceHolder("leave empty for a public (PKCE) client")
	a.clientSecretEntry.SetText(prefs.String(prefClientSecret))

	a.redirectEntry = widget.NewEntry()
	if v := prefs.String(prefRedirect); v != "" {
		a.redirectEntry.SetText(v)
	} else {
		a.redirectEntry.SetText(roam.DefaultRedirectURI)
	}

	a.waitActivity = widget.NewActivity()
	a.waitLabel = widget.NewLabel("")
	a.waitLabel.Wrapping = fyne.TextWrapWord
	a.waitRow = container.NewHBox(a.waitActivity, a.waitLabel)

	a.authURLEntry = widget.NewEntry()
	a.authURLEntry.Disable() // disabled entries remain selectable/copyable
	a.authURLEntry.SetPlaceHolder("authorize URL (copy it manually if the browser did not open)")
	a.authURLRow = container.NewVBox(fieldLabel("Authorize URL"), a.authURLEntry)

	a.connectBtn = widget.NewButton("Connect with Roam", a.onConnect)
	a.connectBtn.Importance = widget.HighImportance

	logo := canvas.NewImageFromResource(AppIcon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(56, 56))
	head := container.New(layout.NewCustomPaddedVBoxLayout(5),
		container.NewCenter(logo),
		container.NewCenter(wordmark()),
		container.NewCenter(tagline("Connect your Roam account")),
	)

	intro := mutedText("Sign in with your own Roam account. This app requests only " +
		roam.ScopeWriteActivity + " and " + roam.ScopeReadActivity +
		"; it can only ever act as you, never on behalf of other users.")

	note := mutedText("Register an OAuth app first (Roam Administration -> Developer -> Add ApiClient) " +
		"and add this exact redirect URI to it:")

	fields := container.New(layout.NewCustomPaddedVBoxLayout(12),
		labeledField("Client ID", a.clientIDEntry),
		labeledField("Client Secret (optional)", a.clientSecretEntry),
		labeledField("Redirect URI", a.redirectEntry),
	)

	card := newSurface(container.New(layout.NewCustomPaddedVBoxLayout(12),
		head,
		intro,
		note,
		fields,
		fullWidth(a.connectBtn),
		a.waitRow,
		a.authURLRow,
	))
	// the card keeps its natural height (Roam-modal style), not the
	// full window height
	a.connectBox = container.New(layout.NewCustomPaddedLayout(28, 24, 14, 14),
		container.NewVBox(card))
	a.waitRow.Hide()
	a.authURLRow.Hide()
}

// wordmark is the app name as drawn on the connect card.
func wordmark() *canvas.Text {
	t := canvas.NewText("roamming", colText)
	t.TextStyle.Bold = true
	t.TextSize = roamThemeInstance.Size(theme.SizeNameHeadingText)
	return t
}

// tagline is the muted line under the wordmark.
func tagline(text string) *canvas.Text {
	t := canvas.NewText(text, colTextMut)
	t.TextSize = 12
	return t
}

// onConnect saves the settings and runs the browser OAuth flow with the
// temporary localhost callback server.
func (a *App) onConnect() {
	clientID := strings.TrimSpace(a.clientIDEntry.Text)
	redirect := strings.TrimSpace(a.redirectEntry.Text)
	if clientID == "" {
		dialog.NewError(errors.New("a Client ID is required (Roam Administration -> Developer)"), a.win)
		return
	}
	prefs := a.fa.Preferences()
	prefs.SetString(prefClientID, clientID)
	prefs.SetString(prefClientSecret, a.clientSecretEntry.Text)
	prefs.SetString(prefRedirect, redirect)

	cfg := roam.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(a.clientSecretEntry.Text),
		RedirectURI:  redirect,
		Timeout:      5 * time.Minute,
	}

	a.connectBtn.Disable()
	a.waitLabel.SetText("Waiting for authorization: finish the sign-in in your browser (expires in 5 minutes).")
	a.waitActivity.Start()
	a.waitRow.Show()
	a.stack.Refresh()

	go func() {
		creds, err := a.auth.Connect(context.Background(), cfg, func(u *url.URL) error {
			fyne.Do(func() { a.authURLEntry.SetText(u.String()) })
			return a.fa.OpenURL(u)
		})
		fyne.Do(func() {
			a.waitActivity.Stop()
			a.waitRow.Hide()
			a.authURLRow.Hide()
			a.connectBtn.Enable()
			if err != nil {
				dialog.NewError(err, a.win)
				return
			}
			a.client = roam.NewClient(a.auth)
			if !creds.HasScope(roam.ScopeWriteActivity) {
				dialog.NewError(fmt.Errorf(
					"the OAuth app did not grant %s; setting activities will fail until its scopes include it",
					roam.ScopeWriteActivity), a.win)
			}
			a.ensureSession()
			a.showActivity()
		})
	}()
}

// ---- activity screen ----------------------------------------------------

func (a *App) buildActivityScreen() {
	a.accountLabel = widget.NewLabel("")
	a.accountLabel.Wrapping = fyne.TextWrapWord
	a.accountLabel.Importance = widget.LowImportance
	disconnectBtn := widget.NewButton("Disconnect", a.onDisconnect)
	disconnectBtn.Importance = widget.LowImportance
	accountRow := container.NewBorder(nil, nil, nil, disconnectBtn, a.accountLabel)

	// Status card: colored presence dot + current activity + the action.
	a.stateLine = widget.NewRichText()
	a.stateLine.Wrapping = fyne.TextWrapWord
	a.setStatusLine(colDotIdle, "No activity set")
	a.countdownLabel = widget.NewLabel("")
	a.countdownLabel.Importance = widget.LowImportance
	a.startStopBtn = widget.NewButton("Start activity", a.onStartStop)
	a.startStopBtn.Importance = widget.HighImportance
	statusCard := titledSurface("Status", container.New(layout.NewCustomPaddedVBoxLayout(8),
		a.stateLine,
		a.countdownLabel,
		fullWidth(a.startStopBtn),
	))

	a.presetSelect = widget.NewSelect(presetLabels(), a.applyPreset)
	a.presetSelect.PlaceHolder = "Pick a preset status…"

	a.emojiEntry = widget.NewEntry()
	a.emojiEntry.SetPlaceHolder("emoji")
	a.emojiEntry.Validator = maxRuneValidator(16)
	pickBtn := widget.NewButton("Pick…", func() {
		showEmojiPicker(a.win, a.emojiEntry.Text, func(e string) { a.emojiEntry.SetText(e) })
	})
	emojiRow := container.NewBorder(nil, nil, nil, pickBtn, a.emojiEntry)

	a.titleEntry = widget.NewEntry()
	a.titleEntry.SetPlaceHolder("status text, shown on hover")
	a.titleEntry.Validator = runeValidator(1, 140)

	a.subtitleEntry = widget.NewEntry()
	a.subtitleEntry.SetPlaceHolder("subtitle (optional)")
	a.subtitleEntry.Validator = maxRuneValidator(140)

	a.colorRow = container.NewGridWrap(fyne.NewSize(40, 40))
	a.buildColorRow()

	a.dndCheck = widget.NewCheck("Also set Do Not Disturb on my office", nil)

	timerLabels := make([]string, len(timerOptions))
	for i, o := range timerOptions {
		timerLabels[i] = o.label
	}
	a.timerSelect = widget.NewSelect(timerLabels, nil)
	a.timerSelect.SetSelected(timerOptions[0].label)

	a.styleSelect = widget.NewSelect(styleLabels(), nil)
	a.styleSelect.SetSelected(styleLabels()[0])

	editorRows := container.New(layout.NewCustomPaddedVBoxLayout(10),
		labeledField("Preset", a.presetSelect),
		labeledField("Emoji", emojiRow),
		labeledField("Status text", a.titleEntry),
		labeledField("Subtitle (optional)", a.subtitleEntry),
		labeledField("Glow color (none = quiet badge)", a.colorRow),
		a.dndCheck,
		container.NewGridWithColumns(2,
			labeledField("Timer", a.timerSelect),
			labeledField("End-time style", a.styleSelect),
		),
	)
	editorCard := titledSurface("Set your external activity", editorRows)

	a.liveList = container.NewVBox()
	refreshLiveBtn := widget.NewButton("Refresh", a.refreshLive)
	refreshLiveBtn.Importance = widget.LowImportance
	liveCard := titledSurface("Live on the map (all integrations)", a.liveList, refreshLiveBtn)

	a.activityBox = container.New(layout.NewCustomPaddedLayout(14, 20, 14, 14),
		container.New(layout.NewCustomPaddedVBoxLayout(12),
			accountRow,
			statusCard,
			editorCard,
			liveCard,
		))
	a.loadLastConfig()
}

// buildColorRow creates one swatch per palette color plus the leading
// "no color" (quiet badge) option.
func (a *App) buildColorRow() {
	a.colorNames = append([]string{""}, roam.Colors...)
	a.colorButtons = make([]*widget.Button, len(a.colorNames))
	for i, name := range a.colorNames {
		name := name
		btn := widget.NewButton("", func() {
			a.colorName = name
			a.refreshSwatches()
		})
		a.colorButtons[i] = btn
		a.colorRow.Add(btn)
	}
	a.refreshSwatches()
}

func (a *App) refreshSwatches() {
	for i, name := range a.colorNames {
		selected := name == a.colorName
		if name == "" {
			a.colorButtons[i].SetIcon(noneSwatchIcon(selected))
			continue
		}
		c, ok := swatchColor(name)
		if !ok {
			continue
		}
		a.colorButtons[i].SetIcon(swatchIcon(c, selected))
	}
}

func (a *App) applyPreset(label string) {
	for _, p := range presets {
		if p.label != label || label == "Custom…" {
			continue
		}
		a.emojiEntry.SetText(p.emoji)
		a.titleEntry.SetText(p.title)
		a.subtitleEntry.SetText("")
		a.colorName = p.color
		a.refreshSwatches()
		a.dndCheck.SetChecked(p.dnd)
		return
	}
}

// buildConfig reads the form into a session.Config.
func (a *App) buildConfig() session.Config {
	return session.Config{
		Display: roam.Display{
			Emoji:    strings.TrimSpace(a.emojiEntry.Text),
			Title:    strings.TrimSpace(a.titleEntry.Text),
			Subtitle: strings.TrimSpace(a.subtitleEntry.Text),
			Color:    a.colorName,
		},
		DND:   a.dndCheck.Checked,
		Timer: a.selectedTimer(),
		Style: a.selectedStyle(),
	}
}

func (a *App) selectedTimer() time.Duration {
	for _, o := range timerOptions {
		if o.label == a.timerSelect.Selected {
			return o.d
		}
	}
	return 0
}

// styleLabels are the user-facing countdown-style options; index maps to
// session.CountdownStyle. Kept short so two fit side by side.
func styleLabels() []string {
	return []string{
		`Fixed ("back at 15:04")`,
		`Live ("back in 24m")`,
	}
}

func (a *App) selectedStyle() session.CountdownStyle {
	if a.styleSelect.Selected == styleLabels()[1] {
		return session.LiveCountdown
	}
	return session.FixedEnd
}

func (a *App) setStyle(s session.CountdownStyle) {
	a.styleSelect.SetSelected(styleLabels()[s])
}

func (a *App) setTimer(minutes int) {
	for _, o := range timerOptions {
		if o.d > 0 && int(o.d.Minutes()) == minutes {
			a.timerSelect.SetSelected(o.label)
			return
		}
	}
	a.timerSelect.SetSelected(timerOptions[0].label)
}

func (a *App) saveLastConfig(cfg session.Config) {
	p := persistedConfig{
		Emoji:    cfg.Display.Emoji,
		Title:    cfg.Display.Title,
		Subtitle: cfg.Display.Subtitle,
		Color:    cfg.Display.Color,
		DND:      cfg.DND,
		TimerMin: int(cfg.Timer.Minutes()),
	}
	if cfg.Style == session.LiveCountdown {
		p.Countdown = "live"
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return
	}
	a.fa.Preferences().SetString(prefLastConfig, string(raw))
}

func (a *App) loadLastConfig() {
	raw := a.fa.Preferences().String(prefLastConfig)
	if raw == "" {
		a.applyPreset("Focusing") // sensible default
		return
	}
	var p persistedConfig
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		a.applyPreset("Focusing")
		return
	}
	a.emojiEntry.SetText(p.Emoji)
	a.titleEntry.SetText(p.Title)
	a.subtitleEntry.SetText(p.Subtitle)
	a.colorName = p.Color
	if !slices.Contains(a.colorNames, p.Color) {
		a.colorName = ""
	}
	a.refreshSwatches()
	a.dndCheck.SetChecked(p.DND)
	a.setTimer(p.TimerMin)
	if p.Countdown == "live" {
		a.setStyle(session.LiveCountdown)
	} else {
		a.setStyle(session.FixedEnd)
	}
}

// ---- actions -------------------------------------------------------------

func (a *App) onStartStop() {
	if a.mgr == nil {
		return
	}
	if a.state.Phase != session.PhaseIdle {
		a.mgr.Stop()
		return
	}
	cfg := a.buildConfig()
	if err := cfg.Validate(); err != nil {
		dialog.NewError(err, a.win)
		return
	}
	a.saveLastConfig(cfg)
	if err := a.mgr.Start(cfg); err != nil {
		dialog.NewError(err, a.win)
	}
}

func (a *App) onDisconnect() {
	dialog.NewConfirm("Disconnect",
		"Revoke the Roam authorization and delete the local tokens?", func(ok bool) {
			if !ok {
				return
			}
			if a.mgr != nil {
				a.mgr.Stop()
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err := a.auth.Disconnect(ctx)
				fyne.Do(func() {
					if err != nil {
						dialog.NewError(fmt.Errorf("revoke failed (local tokens were deleted anyway): %w", err), a.win)
					}
					a.showConnect("")
				})
			}()
		}, a.win)
}

func (a *App) quit() {
	if a.mgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.mgr.StopCtx(ctx) // clear the activity before exiting
	}
	a.fa.Quit()
}

// ---- session glue ----------------------------------------------------------

// ensureSession resolves the user ID (usually already known from
// token.info at connect time) and creates the session manager. Call on
// the UI thread; a slow identity fetch happens in a goroutine.
func (a *App) ensureSession() {
	if userID, _, _ := a.auth.Identity(); userID != "" {
		a.setSession(userID)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		info, err := a.client.TokenInfo(ctx)
		fyne.Do(func() {
			if err != nil {
				dialog.NewError(fmt.Errorf("resolve your user id: %w", err), a.win)
				return
			}
			if err := a.auth.SetIdentity(info); err != nil {
				dialog.NewError(err, a.win)
				return
			}
			a.setSession(info.User.ID)
		})
	}()
}

func (a *App) setSession(userID string) {
	_, userName, roamName := a.auth.Identity()
	a.accountLabel.SetText(fmt.Sprintf("Connected as %s @ %s", userName, roamName))

	m := session.New(a.client, userID)
	m.OnState(func(s session.State) {
		fyne.Do(func() { a.applyState(s) })
	})
	a.mgr = m
}

// applyState renders a session state snapshot (UI thread).
func (a *App) applyState(s session.State) {
	a.state = s
	switch s.Phase {
	case session.PhaseStarting:
		a.setStatusLine(colAmber, "Setting activity…")
		a.countdownLabel.SetText("")
		a.startStopBtn.Importance = widget.MediumImportance
		a.startStopBtn.SetText("Start activity")
		a.startStopBtn.Disable()
	case session.PhaseRunning:
		text := s.Display.Emoji + "  " + s.Display.Title
		if s.DND {
			text += "  ·  DND"
		}
		var dot color.Color = colLime
		if c, ok := swatchColor(s.Display.Color); ok {
			dot = c
		}
		a.setStatusLine(dot, text)
		a.startStopBtn.Importance = widget.DangerImportance
		a.startStopBtn.SetText("Stop activity")
		a.startStopBtn.Enable()
		a.setCountdown(s)
	case session.PhaseStopping:
		a.setStatusLine(colAmber, "Clearing activity…")
		a.startStopBtn.Importance = widget.MediumImportance
		a.startStopBtn.Disable()
	case session.PhaseIdle:
		a.countdownLabel.SetText("")
		dot := colDotIdle
		text := "No activity set"
		if s.LastError != nil {
			dot, text = colRed, "Failed: "+s.LastError.Error()
		}
		a.setStatusLine(dot, text)
		a.startStopBtn.Importance = widget.HighImportance
		a.startStopBtn.SetText("Start activity")
		a.startStopBtn.Enable()
	}
	a.refreshLive()
	a.refreshTray()
}

// setStatusLine renders the Status card line: a colored presence dot
// followed by bold text, on one baseline.
func (a *App) setStatusLine(dot color.Color, text string) {
	a.stateLine.Segments = []widget.RichTextSegment{
		&widget.TextSegment{
			Text:  "●  ",
			Style: widget.RichTextStyle{Inline: true, ColorName: dotColorName(dot), TextStyle: fyne.TextStyle{Bold: true}},
		},
		&widget.TextSegment{
			Text:  text,
			Style: widget.RichTextStyle{Inline: true, ColorName: theme.ColorNameForeground, TextStyle: fyne.TextStyle{Bold: true}},
		},
	}
	a.stateLine.Refresh()
}

// dotColorName maps any color to a theme color name that roamTheme
// resolves back to it, for colored text glyphs.
func dotColorName(c color.Color) fyne.ThemeColorName {
	r, g, b, _ := c.RGBA() // 16-bit premultiplied
	return fyne.ThemeColorName(fmt.Sprintf("dot-%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8)))
}

func (a *App) setCountdown(s session.State) {
	end := s.ServerExpiresAt
	if !s.EndsAt.IsZero() && (end.IsZero() || s.EndsAt.Before(end)) {
		end = s.EndsAt
	}
	if end.IsZero() {
		a.countdownLabel.SetText("")
		return
	}
	if !s.EndsAt.IsZero() && s.EndsAt.Equal(end) {
		a.countdownLabel.SetText(fmt.Sprintf("timer ends in %s (at %s)",
			fmtRemaining(time.Until(end)), end.Local().Format("15:04")))
		return
	}
	a.countdownLabel.SetText(fmt.Sprintf("renews automatically · TTL expires at %s",
		end.Local().Format("15:04")))
}

// tickLoop drives the countdown display while an activity is running.
func (a *App) tickLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		fyne.Do(a.tick)
	}
}

func (a *App) tick() {
	if a.state.Phase != session.PhaseRunning {
		return
	}
	a.setCountdown(a.state)
	a.refreshTray()
}

// liveEntry is one row in the "live on the map" list.
type liveEntry struct {
	color    string // Roam glow palette name; "" = quiet
	emoji    string
	title    string
	subtitle string
	dnd      bool
	expires  time.Time
}

// renderLive redraws the live list (UI thread).
func (a *App) renderLive(entries []liveEntry) {
	a.liveList.Objects = a.liveList.Objects[:0]
	if len(entries) == 0 {
		a.liveList.Add(mutedText("Nothing live right now."))
		a.liveList.Refresh()
		return
	}
	for _, e := range entries {
		a.liveList.Add(liveRow(e))
	}
	a.liveList.Refresh()
}

// renderLiveError replaces the live list with an error note.
func (a *App) renderLiveError(err error) {
	a.liveList.Objects = a.liveList.Objects[:0]
	l := widget.NewLabel("Could not load live activities: " + err.Error())
	l.Importance = widget.DangerImportance
	l.Wrapping = fyne.TextWrapWord
	a.liveList.Add(l)
	a.liveList.Refresh()
}

// liveRow is a Roam-presence-style line: colored dot, title, muted tail.
func liveRow(e liveEntry) fyne.CanvasObject {
	var dot color.Color = colDotIdle
	if c, ok := swatchColor(e.color); ok {
		dot = c
	}
	title := strings.TrimSpace(strings.TrimSpace(e.emoji) + " " + e.title)
	segs := []widget.RichTextSegment{
		&widget.TextSegment{
			Text:  "●",
			Style: widget.RichTextStyle{Inline: true, ColorName: dotColorName(dot)},
		},
		&widget.TextSegment{
			Text:  " " + title,
			Style: widget.RichTextStyle{Inline: true, ColorName: theme.ColorNameForeground},
		},
	}
	var tail strings.Builder
	if e.subtitle != "" {
		tail.WriteString(" · " + e.subtitle)
	}
	if e.dnd {
		tail.WriteString(" · DND")
	}
	if !e.expires.IsZero() {
		fmt.Fprintf(&tail, "  ·  expires %s", e.expires.Local().Format("15:04"))
	}
	if tail.Len() > 0 {
		segs = append(segs, &widget.TextSegment{
			Text:  tail.String(),
			Style: widget.RichTextStyle{Inline: true, ColorName: theme.ColorNameDisabled, SizeName: theme.SizeNameCaptionText},
		})
	}
	text := widget.NewRichText(segs...)
	text.Wrapping = fyne.TextWrapWord
	return text
}

// refreshLive updates the "live on the map" list from user.activity.list.
func (a *App) refreshLive() {
	if a.client == nil || a.mgr == nil || a.liveList == nil {
		return
	}
	userID, _, _ := a.auth.Identity()
	if userID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		acts, err := a.client.ListActivities(ctx, userID)
		fyne.Do(func() {
			if err != nil {
				a.renderLiveError(err)
				return
			}
			entries := make([]liveEntry, len(acts))
			for i, act := range acts {
				entries[i] = liveEntry{
					color:    act.Display.Color,
					emoji:    act.Display.Emoji,
					title:    act.Display.Title,
					subtitle: act.Display.Subtitle,
					dnd:      act.DND,
					expires:  act.ExpiresAt,
				}
			}
			a.renderLive(entries)
		})
	}()
}

// ---- tray ----------------------------------------------------------------

func (a *App) setupTray() {
	desk, ok := a.fa.(desktop.App)
	if !ok {
		return // platform without tray support; window still works
	}
	a.desk = desk

	a.trayStatus = fyne.NewMenuItem("Not connected", nil)
	a.trayStatus.Disabled = true
	a.trayToggle = fyne.NewMenuItem("Start activity", a.onStartStop)
	a.trayToggle.Disabled = true

	a.tray = fyne.NewMenu("Roam Activity",
		fyne.NewMenuItem("Show window", a.Activate),
		fyne.NewMenuItemSeparator(),
		a.trayStatus,
		a.trayToggle,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", a.quit),
	)
	desk.SetSystemTrayMenu(a.tray)
	desk.SetSystemTrayIcon(trayIcon(trayIdle))
	desk.SetSystemTrayWindow(a.win)
}

// refreshTray syncs the tray menu texts and icon with the current state.
// Must run on the UI thread; skips work when nothing changed.
func (a *App) refreshTray() {
	if a.desk == nil {
		return
	}
	status, toggle := "Not connected", "Start activity"
	icon := trayIdle
	enabled := false

	if a.mgr != nil {
		enabled = true
		status, toggle = "No activity", "Start activity"
		switch a.state.Phase {
		case session.PhaseStarting:
			status, enabled = "Setting activity…", false
		case session.PhaseStopping:
			status, enabled = "Clearing activity…", false
		case session.PhaseRunning:
			status, toggle, icon = a.runningTrayText(), "Stop activity", trayActive
		case session.PhaseIdle:
			if a.state.LastError != nil {
				icon = trayError
				status = "Last attempt failed"
			}
		}
	}

	if icon != a.trayIcon {
		a.trayIcon = icon
		a.desk.SetSystemTrayIcon(trayIcon(icon))
	}
	text := status + "\x00" + toggle + "\x00" + fmt.Sprint(enabled)
	if text == a.trayText {
		return
	}
	a.trayText = text
	a.trayStatus.Label = status
	a.trayStatus.Disabled = a.state.Phase == session.PhaseStarting || a.state.Phase == session.PhaseStopping
	a.trayToggle.Label = toggle
	a.trayToggle.Disabled = !enabled
	a.tray.Refresh()
}

func (a *App) runningTrayText() string {
	s := a.state
	end := s.ServerExpiresAt
	if !s.EndsAt.IsZero() && (end.IsZero() || s.EndsAt.Before(end)) {
		end = s.EndsAt
	}
	remaining := "∞"
	if !end.IsZero() {
		remaining = fmtRemaining(time.Until(end))
	}
	return fmt.Sprintf("%s %s · %s left", s.Display.Emoji, s.Display.Title, remaining)
}

// ---- helpers --------------------------------------------------------------

// fmtRemaining renders a duration as "1h05m" / "12m" / "<1m".
func fmtRemaining(d time.Duration) string {
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

// runeValidator requires between min and max Unicode code points.
func runeValidator(min, max int) fyne.StringValidator {
	return func(s string) error {
		if n := utf8.RuneCountInString(s); n < min || n > max {
			return fmt.Errorf("%d–%d characters required", min, max)
		}
		return nil
	}
}

// maxRuneValidator allows empty but caps the length in code points.
func maxRuneValidator(max int) fyne.StringValidator {
	return func(s string) error {
		if n := utf8.RuneCountInString(s); n > max {
			return fmt.Errorf("at most %d characters", max)
		}
		return nil
	}
}
