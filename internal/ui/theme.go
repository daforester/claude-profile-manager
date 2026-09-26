package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// accentTheme keeps Fyne's default light/dark handling but uses a warm
// accent colour.
type accentTheme struct{ fyne.Theme }

var accent = color.NRGBA{R: 0xD9, G: 0x77, B: 0x57, A: 0xFF}

func newTheme() fyne.Theme { return accentTheme{theme.DefaultTheme()} }

func (t accentTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return accent
	case theme.ColorNameSelection:
		return color.NRGBA{R: accent.R, G: accent.G, B: accent.B, A: 0x40}
	}
	return t.Theme.Color(n, v)
}

// vscroll wraps o in a vertical scroller with a right-hand gutter the width
// of the scrollbar. Fyne overlays its scrollbar on the content, so without
// the gutter it covers whatever sits at the right edge.
func vscroll(o fyne.CanvasObject) *container.Scroll {
	gutter := canvas.NewRectangle(color.Transparent)
	gutter.SetMinSize(fyne.NewSize(theme.ScrollBarSize(), 0))
	return container.NewVScroll(container.NewBorder(nil, nil, nil, gutter, o))
}

// parseHex converts #RRGGBB to a colour, falling back to the accent.
func parseHex(s string) color.Color {
	if len(s) != 7 || s[0] != '#' {
		return accent
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		hi, ok1 := hexVal(s[1+2*i])
		lo, ok2 := hexVal(s[2+2*i])
		if !ok1 || !ok2 {
			return accent
		}
		v[i] = hi<<4 | lo
	}
	return color.NRGBA{R: v[0], G: v[1], B: v[2], A: 0xFF}
}

func hexVal(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
