package easteregg

import "testing"

func TestNewRainColumnSpeedAndTrailAreWithinExpectedRanges(t *testing.T) {
	const height = 30
	for col := 0; col < 50; col++ {
		c := NewRainColumn(col, height, 42)
		if c.Speed < 1 || c.Speed > 3 {
			t.Errorf("col %d: Speed = %d, want in [1, 3]", col, c.Speed)
		}
		if c.TrailLen < 1 {
			t.Errorf("col %d: TrailLen = %d, want >= 1", col, c.TrailLen)
		}
		if c.CycleLen < height+c.TrailLen {
			t.Errorf("col %d: CycleLen = %d, want >= height+TrailLen = %d", col, c.CycleLen, height+c.TrailLen)
		}
		if c.Phase < 0 || c.Phase >= c.CycleLen {
			t.Errorf("col %d: Phase = %d, want in [0, %d)", col, c.Phase, c.CycleLen)
		}
	}
}

func TestNewRainColumnIsDeterministic(t *testing.T) {
	a := NewRainColumn(5, 30, 42)
	b := NewRainColumn(5, 30, 42)
	if a != b {
		t.Errorf("NewRainColumn with identical inputs produced different columns: %+v vs %+v", a, b)
	}
}

func TestNewRainColumnsVaryTheirCycleLength(t *testing.T) {
	// Pins the "desync, don't resync into bands" requirement: if every
	// column shared the same CycleLen, they'd eventually realign no
	// matter how randomized Speed/Phase are individually.
	seen := map[int]bool{}
	for col := 0; col < 30; col++ {
		seen[NewRainColumn(col, 30, 42).CycleLen] = true
	}
	if len(seen) < 2 {
		t.Error("expected columns to have varying CycleLen values, got only one distinct value across 30 columns")
	}
}

func TestRainColumnCellAtHeadIsTheBrightestLevel(t *testing.T) {
	c := NewRainColumn(0, 30, 42)
	// Find a frame where the head is safely mid-screen (not clipped by
	// row 0 or height-1), so TrailLen rows above it are all in-bounds.
	frame := 0
	for frame < c.CycleLen {
		head := c.HeadRow(frame)
		if head >= c.TrailLen && head < 30-1 {
			break
		}
		frame++
	}
	head := c.HeadRow(frame)
	_, level, ok := c.CellAt(0, head, frame, 42)
	if !ok {
		t.Fatal("the head row itself should always be part of its own stream")
	}
	if level != RainIntensityLevels {
		t.Errorf("level at the head = %d, want the brightest level %d", level, RainIntensityLevels)
	}
}

func TestRainColumnCellAtFadesTowardTheTrailsEnd(t *testing.T) {
	c := NewRainColumn(0, 30, 42)
	frame := 0
	for frame < c.CycleLen {
		head := c.HeadRow(frame)
		if head >= c.TrailLen && head < 30-1 {
			break
		}
		frame++
	}
	head := c.HeadRow(frame)
	_, headLevel, _ := c.CellAt(0, head, frame, 42)
	_, tailLevel, ok := c.CellAt(0, head-c.TrailLen+1, frame, 42)
	if !ok {
		t.Fatal("the oldest still-visible row should report ok=true")
	}
	if tailLevel >= headLevel {
		t.Errorf("tail level = %d, want strictly dimmer than head level %d", tailLevel, headLevel)
	}
	if tailLevel < 1 {
		t.Errorf("tail level = %d, want >= 1 (ok=true rows never report level 0)", tailLevel)
	}
}

func TestRainColumnCellAtIsEmptyOutsideTheStream(t *testing.T) {
	c := NewRainColumn(0, 30, 42)
	// Far above the head and far below the trail's end, at frame 0,
	// must both report ok=false.
	if _, _, ok := c.CellAt(0, -1000, 0, 42); ok {
		t.Error("a row far above the stream should report ok=false")
	}
	if _, _, ok := c.CellAt(0, 1000, 0, 42); ok {
		t.Error("a row far below the stream should report ok=false")
	}
}

func TestRainColumnCellAtIsDeterministicAcrossCalls(t *testing.T) {
	c := NewRainColumn(0, 30, 42)
	frame := 0
	for frame < c.CycleLen {
		head := c.HeadRow(frame)
		if head >= c.TrailLen && head < 30-1 {
			break
		}
		frame++
	}
	head := c.HeadRow(frame)
	ch1, lvl1, ok1 := c.CellAt(0, head, frame, 42)
	ch2, lvl2, ok2 := c.CellAt(0, head, frame, 42)
	if ch1 != ch2 || lvl1 != lvl2 || ok1 != ok2 {
		t.Errorf("CellAt with identical inputs differed: (%q,%d,%v) vs (%q,%d,%v)", ch1, lvl1, ok1, ch2, lvl2, ok2)
	}
}

// TestRainColumnCellAtCharacterChangesOverTime pins that the character
// shown at the head isn't frozen — comparing the head's own character
// at two different charFrame buckets (RainCharChangeInterval apart)
// across many columns, since any single comparison has a small chance
// (~1/44, the noise pool's own size) of coincidentally matching.
func TestRainColumnCellAtCharacterChangesOverTime(t *testing.T) {
	changed := false
	for col := 0; col < 20; col++ {
		c := NewRainColumn(col, 30, 42)
		frame := 0
		for frame < c.CycleLen {
			head := c.HeadRow(frame)
			if head >= c.TrailLen && head < 15 {
				break
			}
			frame++
		}
		head := c.HeadRow(frame)
		laterFrame := frame + RainCharChangeInterval
		laterHead := c.HeadRow(laterFrame)
		ch1, _, ok1 := c.CellAt(col, head, frame, 42)
		ch2, _, ok2 := c.CellAt(col, laterHead, laterFrame, 42)
		if ok1 && ok2 && ch1 != ch2 {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("expected the head's own character to change across a RainCharChangeInterval boundary in at least one of 20 columns")
	}
}
