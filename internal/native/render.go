// Package native holds OS integrations Fyne does not provide: extra
// system-tray icons (one per profile) and always-on-top windows.
package native

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
)

// LevelColor maps a usage percentage to green / amber / red.
func LevelColor(pct float64) color.NRGBA {
	switch {
	case pct >= 85:
		return color.NRGBA{0xE0, 0x4F, 0x4F, 0xFF}
	case pct >= 60:
		return color.NRGBA{0xE0, 0xA0, 0x30, 0xFF}
	default:
		return color.NRGBA{0x4C, 0xAF, 0x6A, 0xFF}
	}
}

// Icon styles.
const (
	StyleBar     = "bar"
	StylePercent = "percent"
)

// RenderIcon draws a square usage icon of the given size.
// has=false draws a "no data" icon.
func RenderIcon(style string, pct float64, has bool, accent color.Color, size int) *image.NRGBA {
	if size < 16 {
		size = 16
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	acc := color.NRGBAModel.Convert(accent).(color.NRGBA)
	pct = math.Max(0, math.Min(100, pct))
	grey := color.NRGBA{0x9A, 0x9A, 0x9A, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	r := size / 6

	if style == StylePercent {
		fillRounded(img, 0, 0, size, size, r, acc)
		text := "-"
		if has {
			text = strconv.Itoa(int(math.Round(pct)))
		}
		drawDigits(img, text, white, size, size-size/5)
		if has {
			// Thin level bar along the bottom edge.
			h := max(2, size/8)
			w := int(math.Round(float64(size-2*r) * pct / 100))
			fillRect(img, r, size-h-1, r+w, size-1, LevelColor(pct))
		}
		return img
	}

	// Bar style: an outlined "battery" in the profile colour, filled from the
	// bottom in the level colour.
	border := max(1, size/12)
	x0, x1 := size/5, size-size/5
	outline := acc
	if !has {
		outline = grey
	}
	fillRounded(img, x0, 0, x1, size, r/2+1, outline)
	fillRect(img, x0+border, border, x1-border, size-border, color.NRGBA{0x20, 0x20, 0x20, 0xC0})
	if has {
		innerH := size - 2*border
		h := int(math.Round(float64(innerH) * pct / 100))
		if pct > 0 && h < 1 {
			h = 1
		}
		fillRect(img, x0+border, size-border-h, x1-border, size-border, LevelColor(pct))
	}
	return img
}

// PNG encodes img.
func PNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func fillRect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := max(0, y0); y < min(img.Rect.Dy(), y1); y++ {
		for x := max(0, x0); x < min(img.Rect.Dx(), x1); x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func fillRounded(img *image.NRGBA, x0, y0, x1, y1, r int, c color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			dx, dy := 0, 0
			if x < x0+r {
				dx = x0 + r - x
			} else if x >= x1-r {
				dx = x - (x1 - r - 1)
			}
			if y < y0+r {
				dy = y0 + r - y
			} else if y >= y1-r {
				dy = y - (y1 - r - 1)
			}
			if dx*dx+dy*dy <= r*r {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// 3x5 bitmap digits.
var glyphs = map[rune][5]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "001", "001", "001"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	'-': {"000", "000", "111", "000", "000"},
}

// drawDigits centres text horizontally and within the top areaH pixels.
func drawDigits(img *image.NRGBA, text string, c color.NRGBA, size, areaH int) {
	n := len(text)
	// Width in glyph units: 3 per digit + 1 between digits.
	units := 3*n + (n - 1)
	scale := min((size-2)/units, (areaH-2)/5)
	if scale < 1 {
		scale = 1
	}
	w := units * scale
	h := 5 * scale
	ox := (size - w) / 2
	oy := (areaH - h) / 2
	for i, ch := range text {
		g, ok := glyphs[ch]
		if !ok {
			continue
		}
		gx := ox + i*4*scale
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if g[row][col] == '1' {
					fillRect(img, gx+col*scale, oy+row*scale, gx+(col+1)*scale, oy+(row+1)*scale, c)
				}
			}
		}
	}
}
