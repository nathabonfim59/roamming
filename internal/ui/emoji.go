package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// emojiChoices is a curated set of badges that cover the usual statuses.
// Any other emoji can still be typed or pasted into the emoji field.
var emojiChoices = []string{
	"📞", "🎥", "🎯", "💻", "📝", "🧠", "☕", "🍽️",
	"🚗", "✈️", "🚆", "🏖️", "📚", "🎧", "🎮", "🛠️",
	"📦", "🐛", "🔥", "⚡", "🌙", "💤", "🏃", "💪",
	"🧘", "🐶", "🐱", "🌱", "🌧️", "☀️", "💡", "📊",
	"✅", "⏰", "⏳", "🗓️", "📌", "🔒", "🤝", "👋",
	"🎤", "🎬", "📷", "🎨", "🧪", "💬", "📢", "🆘",
	"🚧", "✨", "🤖", "👀", "🕹️", "🧩", "🗺️", "🎓",
}

// showEmojiPicker opens a grid picker dialog and calls onPick with the
// chosen emoji.
func showEmojiPicker(win fyne.Window, current string, onPick func(string)) {
	grid := container.NewGridWithColumns(8)
	for _, e := range emojiChoices {
		e := e
		btn := widget.NewButton(e, func() {
			onPick(e)
		})
		if e == current {
			btn.Importance = widget.HighImportance
		}
		grid.Add(btn)
	}

	content := container.NewVBox(
		grid,
		widget.NewLabel("…or type/paste any emoji in the field."),
	)
	d := dialog.NewCustom("Choose an emoji", "Close", content, win)
	d.Resize(fyne.NewSize(380, 340))
	d.Show()
}
