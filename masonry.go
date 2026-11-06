package main

import "math"

// Provisional design metrics, in logical units.
const (
	masonryMinimum = float32(200)
	masonryGap     = float32(12)
	masonryPadding = float32(16)
	masonryBuffer  = float32(320)
)

type cardRect struct{ X, Y, W, H float32 }
type masonryLayout struct {
	Cards  []cardRect
	Height float32
}

func layoutMasonry(items []capture, width, minimum float32) masonryLayout {
	inner := max(0, width-2*masonryPadding)
	n := max(1, int(math.Floor(float64((inner+masonryGap)/(minimum+masonryGap)))))
	w := max(0, (inner-float32(n-1)*masonryGap)/float32(n))
	bottom := make([]float32, n)
	for i := range bottom {
		bottom[i] = masonryPadding
	}
	layout := masonryLayout{Cards: make([]cardRect, len(items)), Height: 2 * masonryPadding}
	for i, item := range items {
		col := 0
		for j := 1; j < n; j++ {
			if bottom[j] < bottom[col] {
				col = j
			}
		}
		h := w * float32(max(1, item.Height)) / float32(max(1, item.Width))
		if item.isLink() {
			h += linkCaptionHeight
		}
		layout.Cards[i] = cardRect{masonryPadding + float32(col)*(w+masonryGap), bottom[col], w, h}
		bottom[col] += h + masonryGap
		layout.Height = max(layout.Height, bottom[col]-masonryGap+masonryPadding)
	}
	return layout
}

func visibleCard(r cardRect, offset, height float32) bool {
	return r.Y+r.H >= offset-masonryBuffer && r.Y <= offset+height+masonryBuffer
}
