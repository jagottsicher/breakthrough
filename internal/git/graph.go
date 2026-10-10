package git

// LaneGlyph identifies how a single column should render within one
// row of the commit graph — pure layout, no color and no tcell/tview
// dependency: internal/ui maps each glyph to an actual rune and each
// Lane id to a color.
type LaneGlyph int

const (
	LaneGlyphNone LaneGlyph = iota
	LaneGlyphPass
	LaneGlyphNode
	LaneGlyphHorizontal
	LaneGlyphForkRight // a new or reused lane branches off to the right of the node and continues below
	LaneGlyphForkLeft  // mirrored, to the left
	LaneGlyphJoinRight // a lane arriving from above, sitting right of the node, bends left and ends here
	LaneGlyphJoinLeft  // mirrored, the closing lane sits left of the node
)

// LaneCell is one column's own state within one LaneRow.
type LaneCell struct {
	Glyph LaneGlyph
	Lane  int // stable lane id for color purposes; -1 when Glyph is LaneGlyphNone
}

// LaneRow is one commit's own row in the rendered graph — exactly one
// row per LogEntry, in the same order CommitLanes was given. This
// function never inserts or removes rows, only decorates each one, so
// gitBreachCommitAt's direct row->index mapping and
// maybeLoadMoreGitBreachCommits's pagination both keep working
// unchanged against the result.
type LaneRow struct {
	Commit LogEntry
	Column int
	Cells  []LaneCell
}

// MaxGraphLanes caps how many columns a row's own Cells slice is
// trimmed to for rendering sanity on a wide-fanout repository — the
// commit's own node (Column) is always kept even past the cap, since
// hiding the row's own commit would be worse than a wider row;
// everything else here is display-only, lane bookkeeping itself is
// never affected by this constant.
const MaxGraphLanes = 8

type laneState struct {
	hash string
	id   int
}

// CommitLanes assigns each commit in commits (newest first, the same
// order Log returns) to a lane/column and records, per row, every
// other column's own state at that row — pass-through lanes, forks (a
// merge commit's extra parents branching into a new or an already
// tracked column) and joins (two lanes converging on a shared
// ancestor, the commit a branch was originally cut from). Pure
// function over LogEntry.Parents, no git invocation of its own.
//
// Processing walks newest to oldest, the same direction commits is
// already sorted in, so a commit's own parents are only ever resolved
// after the commit itself has been placed in a column — a commit can
// only be an ancestor of something earlier in the list, never a
// descendant of something later, in any order a real `git log`
// produces here.
func CommitLanes(commits []LogEntry) []LaneRow {
	var lanes []laneState
	nextID := 0
	rows := make([]LaneRow, 0, len(commits))

	indexOf := func(hash string) int {
		for i, l := range lanes {
			if l.hash == hash {
				return i
			}
		}
		return -1
	}
	firstEmpty := func() int {
		for i, l := range lanes {
			if l.hash == "" {
				return i
			}
		}
		return -1
	}

	for _, c := range commits {
		// Every lane already pointing at this commit — normally just
		// one (whichever child set it as its own parent), but two or
		// more lanes can share the same next commit when separate
		// branches converge back on a common ancestor.
		var matches []int
		for i, l := range lanes {
			if l.hash == c.Hash {
				matches = append(matches, i)
			}
		}

		var col int
		if len(matches) > 0 {
			col = matches[0]
		} else {
			col = firstEmpty()
			if col == -1 {
				col = len(lanes)
				lanes = append(lanes, laneState{})
			}
			lanes[col] = laneState{hash: c.Hash, id: nextID}
			nextID++
		}

		cells := make([]LaneCell, len(lanes))
		for i, l := range lanes {
			switch {
			case i == col:
				cells[i] = LaneCell{Glyph: LaneGlyphNode, Lane: l.id}
			case l.hash != "":
				cells[i] = LaneCell{Glyph: LaneGlyphPass, Lane: l.id}
			default:
				cells[i] = LaneCell{Glyph: LaneGlyphNone, Lane: -1}
			}
		}

		// Close every other matching lane, drawing its own join into
		// col — the row where two branches' histories become one.
		if len(matches) > 1 {
			for _, j := range matches[1:] {
				drawCorner(cells, j, col, lanes[j].id, true)
				lanes[j] = laneState{}
			}
		}

		if len(c.Parents) == 0 {
			lanes[col] = laneState{}
			rows = append(rows, LaneRow{Commit: c, Column: col, Cells: trimCells(cells, col)})
			continue
		}

		lanes[col] = laneState{hash: c.Parents[0], id: lanes[col].id}

		for _, p := range c.Parents[1:] {
			if j := indexOf(p); j >= 0 && j != col {
				drawCorner(cells, j, col, lanes[j].id, false)
				continue
			}
			target := firstEmpty()
			if target == -1 || target == col {
				target = len(lanes)
				lanes = append(lanes, laneState{})
				cells = append(cells, LaneCell{Glyph: LaneGlyphNone, Lane: -1})
			}
			lanes[target] = laneState{hash: p, id: nextID}
			nextID++
			drawCorner(cells, target, col, lanes[target].id, false)
		}

		rows = append(rows, LaneRow{Commit: c, Column: col, Cells: trimCells(cells, col)})
	}

	return rows
}

// drawCorner fills the horizontal run and the bend between from and
// col with the matching box-drawing glyphs. join == true draws a lane
// that closes here (a vertical line arriving from above bends into
// col and ends, a convergence); join == false draws a lane that's new
// or continuing here (bends away from col and continues below, either
// freshly forked off a merge commit or an already tracked lane one of
// its extra parents reuses).
func drawCorner(cells []LaneCell, from, col, lane int, join bool) {
	lo, hi := col, from
	if from < col {
		lo, hi = from, col
	}
	for i := lo + 1; i < hi; i++ {
		// A nearer connector's own corner at this column takes
		// precedence over a farther connector's horizontal run merely
		// passing through it (an octopus merge's second and third
		// fork both crossing the first fork's own column, say) — only
		// an otherwise plain pass-through or empty column becomes
		// part of this run.
		if cells[i].Glyph == LaneGlyphNone || cells[i].Glyph == LaneGlyphPass {
			cells[i] = LaneCell{Glyph: LaneGlyphHorizontal, Lane: lane}
		}
	}
	switch {
	case join && from > col:
		cells[from] = LaneCell{Glyph: LaneGlyphJoinRight, Lane: lane}
	case join && from < col:
		cells[from] = LaneCell{Glyph: LaneGlyphJoinLeft, Lane: lane}
	case !join && from > col:
		cells[from] = LaneCell{Glyph: LaneGlyphForkRight, Lane: lane}
	default:
		cells[from] = LaneCell{Glyph: LaneGlyphForkLeft, Lane: lane}
	}
}

// trimCells drops trailing empty columns past the last real lane (or
// past the node's own column, whichever is further right) so a row
// with few active lanes doesn't carry dead width, then caps the
// result at MaxGraphLanes — never past the node's own column, so a
// pathologically wide fan-out still shows the commit itself instead of
// clipping it away.
func trimCells(cells []LaneCell, col int) []LaneCell {
	last := col
	for i := len(cells) - 1; i > last; i-- {
		if cells[i].Glyph != LaneGlyphNone {
			last = i
			break
		}
	}
	cells = cells[:last+1]
	if len(cells) > MaxGraphLanes && col < MaxGraphLanes {
		cells = cells[:MaxGraphLanes]
	}
	return cells
}
