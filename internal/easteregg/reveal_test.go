package easteregg

import (
	"math"
	"testing"
)

func TestRevealMaskFrameZeroRevealsNothing(t *testing.T) {
	const width, height = 80, 24
	for row := 0; row < height; row += 3 {
		for col := 0; col < width; col += 3 {
			if inside, _ := RevealMask(col, row, 0, 40, width, height, 42); inside {
				t.Fatalf("cell (%d,%d) inside at frame 0, want nothing revealed yet", col, row)
			}
		}
	}
}

func TestRevealMaskFinalFrameRevealsEveryCellIncludingCorners(t *testing.T) {
	const width, height = 80, 24
	corners := [][2]int{{0, 0}, {width - 1, 0}, {0, height - 1}, {width - 1, height - 1}}
	for _, c := range corners {
		if inside, _ := RevealMask(c[0], c[1], 40, 40, width, height, 42); !inside {
			t.Errorf("corner (%d,%d) not inside at the final frame, want the whole field revealed by then", c[0], c[1])
		}
	}
	// Also sample the center and edge midpoints for good measure.
	points := [][2]int{{width / 2, height / 2}, {width / 2, 0}, {width / 2, height - 1}, {0, height / 2}, {width - 1, height / 2}}
	for _, p := range points {
		if inside, _ := RevealMask(p[0], p[1], 40, 40, width, height, 42); !inside {
			t.Errorf("point (%d,%d) not inside at the final frame", p[0], p[1])
		}
	}
}

func TestRevealMaskIsDeterministic(t *testing.T) {
	in1, e1 := RevealMask(40, 12, 20, 40, 80, 24, 42)
	in2, e2 := RevealMask(40, 12, 20, 40, 80, 24, 42)
	if in1 != in2 || e1 != e2 {
		t.Errorf("RevealMask with identical inputs differed: (%v,%v) vs (%v,%v)", in1, e1, in2, e2)
	}
}

func TestRevealMaskOnlyEverGrows(t *testing.T) {
	const width, height, totalFrames = 80, 24, 40
	const seed = 42
	for row := 0; row < height; row += 2 {
		for col := 0; col < width; col += 2 {
			wasInside := false
			for frame := 0; frame <= totalFrames; frame++ {
				inside, _ := RevealMask(col, row, frame, totalFrames, width, height, seed)
				if wasInside && !inside {
					t.Fatalf("cell (%d,%d) was inside at an earlier frame but outside at frame %d — the mask must never shrink", col, row, frame)
				}
				wasInside = wasInside || inside
			}
		}
	}
}

func TestRevealMaskEdgeClosenessIsHighestNearTheBoundary(t *testing.T) {
	const width, height = 80, 24
	// Centre is always deep inside once revealed at all.
	for frame := 10; frame <= 40; frame += 10 {
		if inside, edge := RevealMask(width/2, height/2, frame, 40, width, height, 42); inside && edge > 0.3 {
			t.Errorf("frame %d: center edgeCloseness = %v, want close to 0 (deep inside)", frame, edge)
		}
	}
	// A cell just barely inside the mask (scan outward from the center
	// along one ray until we find the first outside cell, then check
	// just inside it) should report a high edgeCloseness.
	frame := 20
	row := height / 2
	lastInsideCol := -1
	for col := width / 2; col < width; col++ {
		inside, _ := RevealMask(col, row, frame, 40, width, height, 42)
		if !inside {
			break
		}
		lastInsideCol = col
	}
	if lastInsideCol == -1 {
		t.Fatal("setup: expected at least one inside cell along this ray at frame 20")
	}
	_, edge := RevealMask(lastInsideCol, row, frame, 40, width, height, 42)
	if edge < 0.5 {
		t.Errorf("edgeCloseness at the last inside cell before the boundary = %v, want >= 0.5 (near the edge)", edge)
	}
}

func TestRevealMaskBoundaryIsJaggedNotACircle(t *testing.T) {
	// Pins that revealJag actually varies the radius by angle — if it
	// didn't, every direction would reveal to the exact same distance
	// at a given frame, which is what this checks isn't the case.
	const width, height = 80, 24
	frame := 15
	var radii []float64
	for _, angleDeg := range []int{0, 45, 90, 135, 180, 225, 270, 315} {
		// Walk outward from center at this angle until we leave the mask.
		rad := float64(angleDeg) * math.Pi / 180
		steps := 0
		for steps < 100 {
			col := width/2 + int(float64(steps)*math.Cos(rad))
			row := height/2 + int(float64(steps)*math.Sin(rad)/revealAspectCorrection)
			if col < 0 || col >= width || row < 0 || row >= height {
				break
			}
			inside, _ := RevealMask(col, row, frame, 40, width, height, 42)
			if !inside {
				break
			}
			steps++
		}
		radii = append(radii, float64(steps))
	}
	allSame := true
	for _, r := range radii[1:] {
		if r != radii[0] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("the mask's own radius is identical in every sampled direction — expected a jagged, non-circular boundary")
	}
}
