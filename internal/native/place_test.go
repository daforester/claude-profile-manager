package native

import "testing"

func TestFit(t *testing.T) {
	left := Rect{0, 0, 1920, 1040}     // primary, taskbar below
	right := Rect{1920, 0, 2560, 1400} // second monitor
	areas := []Rect{left, right}
	for _, c := range []struct {
		name  string
		in    Rect
		want  Rect
		areas []Rect
	}{
		{"inside stays", Rect{100, 100, 800, 600}, Rect{100, 100, 800, 600}, areas},
		{"on second monitor stays", Rect{2000, 50, 800, 600}, Rect{2000, 50, 800, 600}, areas},
		{"hanging off the bottom moves up", Rect{100, 900, 800, 600}, Rect{100, 440, 800, 600}, areas},
		{"straddling picks the bigger overlap", Rect{1700, 100, 800, 600}, Rect{1920, 100, 800, 600}, areas},
		{"monitor gone moves to primary", Rect{5000, 200, 800, 600}, Rect{1120, 200, 800, 600}, areas},
		{"negative coords move in", Rect{-500, -300, 800, 600}, Rect{0, 0, 800, 600}, areas},
		{"too big shrinks", Rect{100, 100, 3000, 2000}, Rect{0, 0, 1920, 1040}, []Rect{left}},
		{"no areas unchanged", Rect{-500, 0, 10, 10}, Rect{-500, 0, 10, 10}, nil},
	} {
		if got := Fit(c.in, c.areas); got != c.want {
			t.Errorf("%s: Fit(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}
