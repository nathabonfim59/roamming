package ui

import (
	_ "embed"
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The Roam design language (see docs/design and the Roam desktop
// clients): near-black layered surfaces, rounded cards with hairline
// borders, pill-shaped buttons, muted secondary text and the signature
// lime accent (red for destructive actions). This theme forces that
// dark look regardless of the system preference, like the Roam apps.

// Inter faces (SIL OFL 1.1, see fonts/OFL.txt), bundled so the look
// does not depend on fonts installed on the user's system.
var (
	//go:embed fonts/inter-400.ttf
	fontInterRegular []byte
	//go:embed fonts/inter-700.ttf
	fontInterBold []byte
	//go:embed fonts/inter-400-italic.ttf
	fontInterItalic []byte
)

// Palette, sampled from the Roam clients.
var (
	colWindow  = color.NRGBA{0x0f, 0x11, 0x14, 0xff} // window background
	colSurface = color.NRGBA{0x17, 0x1b, 0x20, 0xff} // cards
	colBorder  = color.NRGBA{0x28, 0x2e, 0x36, 0xff} // hairline card/input borders
	colButton  = color.NRGBA{0x24, 0x2a, 0x31, 0xff} // buttons, selects, swatch tiles
	// Hover/press are translucent overlays: Fyne alpha-blends them over
	// the button's own background, so the button just lightens (hover)
	// or darkens (tap) and its text color — dark on lime, white on gray
	// — stays legible.
	colHover     = color.NRGBA{0xff, 0xff, 0xff, 0x14}
	colPressed   = color.NRGBA{0x00, 0x00, 0x00, 0x33}
	colDisButton = color.NRGBA{0x1a, 0x1e, 0x24, 0xff}
	colInput     = color.NRGBA{0x1e, 0x23, 0x2a, 0xff} // entry fields

	colText        = color.NRGBA{0xf0, 0xf2, 0xf5, 0xff}
	colTextMut     = color.NRGBA{0x8e, 0x95, 0x9d, 0xff} // secondary/disabled text
	colPlaceholder = color.NRGBA{0x6b, 0x72, 0x7b, 0xff}

	colLime   = color.NRGBA{0xc9, 0xf3, 0x4b, 0xff} // Roam accent
	colOnLime = color.NRGBA{0x14, 0x1a, 0x06, 0xff}
	colRed    = color.NRGBA{0xe5, 0x48, 0x4d, 0xff} // destructive (leave/stop)
	colGreen  = color.NRGBA{0x43, 0xd1, 0x7c, 0xff}
	colAmber  = color.NRGBA{0xf5, 0xb5, 0x44, 0xff}

	colMenu      = color.NRGBA{0x1b, 0x1f, 0x26, 0xff} // dropdowns
	colOverlay   = color.NRGBA{0x18, 0x1c, 0x22, 0xff} // dialogs
	colSeparator = color.NRGBA{0x26, 0x2c, 0x33, 0xff}
	colShadow    = color.NRGBA{0x00, 0x00, 0x00, 0x55}
	colDotIdle   = color.NRGBA{0x45, 0x4c, 0x55, 0xff} // presence dot, no activity
)

// roamTheme implements fyne.Theme with the Roam dark look. It is
// stateless; use roamThemeInstance.
type roamTheme struct{}

var roamThemeInstance fyne.Theme = roamTheme{}

var (
	fontRegularRes = fyne.NewStaticResource("inter-regular.ttf", fontInterRegular)
	fontBoldRes    = fyne.NewStaticResource("inter-bold.ttf", fontInterBold)
	fontItalicRes  = fyne.NewStaticResource("inter-italic.ttf", fontInterItalic)
)

func (roamTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	// Colored "●" glyphs: names of the form "dot-rrggbb" resolve to
	// that exact color (see dotColorName).
	if hex, ok := strings.CutPrefix(string(n), "dot-"); ok && len(hex) == 6 {
		var r, g, b uint8
		if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err == nil {
			return color.NRGBA{r, g, b, 0xff}
		}
	}
	switch n {
	case theme.ColorNameBackground:
		return colWindow
	case theme.ColorNameButton:
		return colButton
	case theme.ColorNameDisabledButton:
		return colDisButton
	case theme.ColorNameDisabled:
		return colTextMut
	case theme.ColorNameError:
		return colRed
	case theme.ColorNameFocus:
		return color.NRGBA{0xc9, 0xf3, 0x4b, 0x30}
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNameForegroundOnPrimary:
		return colOnLime
	case theme.ColorNameForegroundOnError:
		return color.NRGBA{0xff, 0xff, 0xff, 0xff}
	case theme.ColorNameForegroundOnSuccess:
		return colOnLime
	case theme.ColorNameForegroundOnWarning:
		return colOnLime
	case theme.ColorNameHover:
		return colHover
	case theme.ColorNamePressed:
		return colPressed
	case theme.ColorNameHyperlink:
		return colLime
	case theme.ColorNameInputBackground:
		return colInput
	case theme.ColorNameInputBorder:
		return colBorder
	case theme.ColorNameMenuBackground:
		return colMenu
	case theme.ColorNameOverlayBackground:
		return colOverlay
	case theme.ColorNamePlaceHolder:
		return colPlaceholder
	case theme.ColorNamePrimary:
		return colLime
	case theme.ColorNameScrollBar:
		return color.NRGBA{0xc0, 0xc6, 0xcd, 0x40}
	case theme.ColorNameScrollBarBackground:
		return color.NRGBA{0, 0, 0, 0}
	case theme.ColorNameSelection:
		return color.NRGBA{0xc9, 0xf3, 0x4b, 0x44}
	case theme.ColorNameSeparator:
		return colSeparator
	case theme.ColorNameShadow:
		return colShadow
	case theme.ColorNameSuccess:
		return colGreen
	case theme.ColorNameWarning:
		return colAmber
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

func (roamTheme) Font(style fyne.TextStyle) fyne.Resource {
	switch {
	case style.Monospace, style.Symbol:
		return theme.DefaultTheme().Font(style) // Fyne's bundled mono/symbol faces
	case style.Bold && style.Italic:
		return fontBoldRes // no bold-italic face bundled; bold is the closest
	case style.Bold:
		return fontBoldRes
	case style.Italic:
		return fontItalicRes
	}
	return fontRegularRes
}

func (roamTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n) // Fyne's bundled widget icons
}

func (roamTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	case theme.SizeNameSubHeadingText:
		return 16
	case theme.SizeNameCaptionText:
		return 11.5
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameLineSpacing:
		return 4
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameButtonRadius:
		return 18 // pill-shaped buttons
	case theme.SizeNameCardRadius:
		return 14
	case theme.SizeNameInputRadius:
		return 9
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameMenuRadius:
		return 12
	case theme.SizeNameDialogRadius:
		return 16
	case theme.SizeNamePopupRadius:
		return 12
	}
	return theme.DefaultTheme().Size(n)
}
