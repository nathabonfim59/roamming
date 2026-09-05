package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// cardPad is the inset between a surface's edge and its content.
const cardPad float32 = 16

// surface is a rounded panel with a hairline border: the Roam-style
// card that groups each part of the UI.
type surface struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

func newSurface(content fyne.CanvasObject) *surface {
	s := &surface{content: content}
	s.ExtendBaseWidget(s)
	return s
}

func (s *surface) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(colSurface)
	bg.CornerRadius = roamThemeInstance.Size(theme.SizeNameCardRadius)
	edge := &canvas.Rectangle{
		StrokeColor:  colBorder,
		StrokeWidth:  1,
		CornerRadius: bg.CornerRadius,
	}
	return &surfaceRenderer{bg: bg, edge: edge, content: s.content}
}

type surfaceRenderer struct {
	bg, edge *canvas.Rectangle
	content  fyne.CanvasObject
}

func (r *surfaceRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.edge.Resize(size)
	inset := cardPad
	r.content.Move(fyne.NewPos(inset, inset))
	r.content.Resize(size.SubtractWidthHeight(2*inset, 2*inset))
}

func (r *surfaceRenderer) MinSize() fyne.Size {
	return r.content.MinSize().AddWidthHeight(2*cardPad, 2*cardPad)
}

func (r *surfaceRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.edge, r.content}
}

func (r *surfaceRenderer) Destroy() {}

func (r *surfaceRenderer) Refresh() {
	r.content.Refresh()
	canvas.Refresh(r.bg)
	canvas.Refresh(r.edge)
}

// titledSurface builds a card with a heading row and a body; an
// optional trailing control (e.g. a ghost button) sits at the right of
// the heading, like Roam's panel headers.
func titledSurface(title string, body fyne.CanvasObject, trailing ...fyne.CanvasObject) *surface {
	var header fyne.CanvasObject = cardTitle(title)
	if len(trailing) > 0 {
		header = container.NewBorder(nil, nil, nil, trailing[0], header)
	}
	content := container.New(
		layout.NewCustomPaddedVBoxLayout(10),
		header,
		body,
	)
	return newSurface(content)
}

// cardTitle is a section heading inside a card.
func cardTitle(text string) *canvas.Text {
	t := canvas.NewText(text, colText)
	t.TextStyle.Bold = true
	t.TextSize = 15
	return t
}

// fieldLabel is a small muted caption above a form field.
func fieldLabel(text string) *canvas.Text {
	t := canvas.NewText(strings.ToUpper(text), colTextMut)
	t.TextSize = roamThemeInstance.Size(theme.SizeNameCaptionText)
	return t
}

// mutedText is a secondary wrapped paragraph (descriptions, notes).
func mutedText(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

// labeledField stacks a caption over an input, Roam-form style.
func labeledField(label string, w fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedVBoxLayout(6), fieldLabel(label), w)
}

// fullWidth stretches a control (e.g. the primary action button) across
// the card width; vertical size stays at the control's minimum.
func fullWidth(w fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWithColumns(1, w)
}
