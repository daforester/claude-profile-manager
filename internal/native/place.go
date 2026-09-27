package native

// Rect is an area of the screen in pixels.
type Rect struct{ X, Y, W, H int }

func (r Rect) overlap(o Rect) int {
	w := min(r.X+r.W, o.X+o.W) - max(r.X, o.X)
	h := min(r.Y+r.H, o.Y+o.H) - max(r.Y, o.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// Fit places r inside one of the given work areas (the usable part of each
// monitor): the one it overlaps most, or the first (primary) if it overlaps
// none, e.g. because that monitor has gone. r shrinks to fit the area if it
// is too big, then moves just far enough to lie inside it. With no areas, r
// is returned unchanged.
func Fit(r Rect, areas []Rect) Rect {
	if len(areas) == 0 {
		return r
	}
	a, best := areas[0], 0
	for _, o := range areas {
		if n := r.overlap(o); n > best {
			a, best = o, n
		}
	}
	r.W, r.H = min(r.W, a.W), min(r.H, a.H)
	r.X = min(max(r.X, a.X), a.X+a.W-r.W)
	r.Y = min(max(r.Y, a.Y), a.Y+a.H-r.H)
	return r
}
