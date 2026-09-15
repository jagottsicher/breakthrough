package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func newTestRootForConnectionMenu(t *testing.T) *Root {
	t.Helper()
	withTestConfigHome(t)
	dir := t.TempDir()
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

func itemTexts(l *tview.List) []string {
	texts := make([]string, l.GetItemCount())
	for i := range texts {
		texts[i], _ = l.GetItemText(i)
	}
	return texts
}

func TestRenderConnectionMenuOnALocalPanelOffersOnlyNewConnection(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	if len(texts) != 1 || !strings.Contains(texts[0], "New connection") {
		t.Errorf("items = %v, want exactly one \"New connection…\" row for a local panel", texts)
	}
}

func TestRenderConnectionMenuOnAConnectedPanelOffersDisconnectFirst(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	client := fakeConnectedClient()
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := r.panel.connectRemote(client, conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	if len(texts) == 0 || !strings.Contains(texts[0], "Disconnect") || !strings.Contains(texts[0], conn.Label()) {
		t.Fatalf("items[0] = %q, want a \"Disconnect (...)\" row naming the active connection", texts[0])
	}
	if !strings.Contains(texts[1], "New connection") {
		t.Errorf("items[1] = %q, want \"New connection…\" right after Disconnect", texts[1])
	}
}

func TestRenderConnectionMenuListsHistoryColoredByState(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	ok := remotefs.Connection{Host: "ok.example.com", User: "tester"}
	failed := remotefs.Connection{Host: "failed.example.com", User: "tester"}
	if err := remotefs.RecordAttempt(failed, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := remotefs.RecordAttempt(ok, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	r.renderConnectionMenu()

	texts := itemTexts(r.connectionMenuList)
	var okLine, failedLine string
	for _, text := range texts {
		if strings.Contains(text, ok.Label()) {
			okLine = text
		}
		if strings.Contains(text, failed.Label()) {
			failedLine = text
		}
	}
	if okLine == "" || failedLine == "" {
		t.Fatalf("items = %v, want both history entries listed", texts)
	}
	if !strings.Contains(failedLine, colorTag(r.theme.CriticalText)) {
		t.Errorf("failed entry %q does not carry the critical color tag", failedLine)
	}
	if strings.Contains(okLine, colorTag(r.theme.CriticalText)) {
		t.Errorf("successful entry %q incorrectly carries the critical color tag", okLine)
	}
}

func TestRenderConnectionMenuColorsTheCurrentlyActiveEntryGreen(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := r.panel.connectRemote(fakeConnectedClient(), conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	r.renderConnectionMenu()

	var line string
	for _, text := range itemTexts(r.connectionMenuList) {
		if strings.Contains(text, conn.Label()) && !strings.Contains(text, "Disconnect") {
			line = text
		}
	}
	if line == "" {
		t.Fatal("history row for the active connection not found")
	}
	if !strings.Contains(line, colorTag(r.theme.EntryExecutable)) {
		t.Errorf("active connection's history row %q does not carry the connected color tag", line)
	}
}

// TestRenderConnectionMenuColorsAPreviouslySuccessfulEntryAMutedGreen
// pins the user's own explicit report: a connection that isn't
// currently active, isn't the last-failed one either, must still read
// as green (just a dimmer shade — see connectionHistorySuccessBlend's
// own doc comment) — never the dropdown's own plain, uncolored text,
// which reads as "unknown" rather than "this one has worked before".
func TestRenderConnectionMenuColorsAPreviouslySuccessfulEntryAMutedGreen(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := remotefs.RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	// Deliberately never connected in this panel — this exercises the
	// "succeeded before, not active right now" branch specifically,
	// not the "currently active" one.

	r.renderConnectionMenu()

	var line string
	for _, text := range itemTexts(r.connectionMenuList) {
		if strings.Contains(text, conn.Label()) {
			line = text
		}
	}
	if line == "" {
		t.Fatal("history row for the successful connection not found")
	}
	if strings.Contains(line, colorTag(r.theme.EntryExecutable)) {
		t.Error("an inactive entry carries the full-brightness connected color tag, want a dimmer shade")
	}
	if strings.Contains(line, colorTag(r.theme.CriticalText)) {
		t.Error("a successful entry carries the failed/critical color tag")
	}
	wantColor := blendToward(r.theme.EntryExecutable, colorBlack, connectionHistorySuccessBlend)
	if !strings.Contains(line, colorTag(wantColor)) {
		t.Errorf("row %q does not carry the expected muted-green color tag %q", line, colorTag(wantColor))
	}
}
