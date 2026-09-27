//go:build cgo && (windows || darwin || ((linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11)))

package native

import "github.com/go-gl/glfw/v3.4/glfw"

// WorkAreas returns the usable part of each monitor (without taskbars and
// panels) in screen pixels, primary first. Call it on the main thread.
func WorkAreas() (areas []Rect) {
	defer func() {
		if recover() != nil { // GLFW not initialised
			areas = nil
		}
	}()
	for _, m := range glfw.GetMonitors() {
		x, y, w, h := m.GetWorkarea()
		if w > 0 && h > 0 {
			areas = append(areas, Rect{x, y, w, h})
		}
	}
	return areas
}
