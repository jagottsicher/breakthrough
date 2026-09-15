package ui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// fakeRemoteClient is a minimal, in-memory remotefs.Client double —
// Panel's own remote-wiring tests need something to attach as p.remote,
// but exercising a real SFTP round trip belongs to internal/remotefs's
// own test suite (see its testserver_test.go), not here: this package
// only needs to prove Panel branches correctly on *whether* p.remote is
// set, not that a real wire protocol works.
type fakeRemoteClient struct {
	root    string
	entries map[string][]fsops.Entry
	closed  bool
}

var _ remotefs.Client = (*fakeRemoteClient)(nil)

func (f *fakeRemoteClient) Root() string { return f.root }
func (f *fakeRemoteClient) ListDir(path string) ([]fsops.Entry, error) {
	return f.entries[path], nil
}
func (f *fakeRemoteClient) Stat(path string) (fsops.Entry, error) {
	return fsops.Entry{}, errors.New("fakeRemoteClient: Stat not implemented")
}
func (f *fakeRemoteClient) Open(string) (io.ReadCloser, error) {
	return nil, errors.New("fakeRemoteClient: Open not implemented")
}
func (f *fakeRemoteClient) Create(string) (io.WriteCloser, error) {
	return nil, errors.New("fakeRemoteClient: Create not implemented")
}
func (f *fakeRemoteClient) Mkdir(string) error           { return nil }
func (f *fakeRemoteClient) Remove(string) error          { return nil }
func (f *fakeRemoteClient) RemoveDirectory(string) error { return nil }
func (f *fakeRemoteClient) Rename(string, string) error  { return nil }
func (f *fakeRemoteClient) Close() error {
	f.closed = true
	return nil
}

func TestConnectRemoteSwitchesToTheRemoteRootAndResetsHistory(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	if err := p.navigate(dir); err != nil { // give the local history more than one entry first
		t.Fatalf("navigate: %v", err)
	}

	client := &fakeRemoteClient{root: "/home/tester", entries: map[string][]fsops.Entry{
		"/home/tester": {{Name: "readme.txt", Type: fsops.TypeFile}},
	}}
	conn := remotefs.Connection{Host: "example.com", User: "tester"}
	if err := p.connectRemote(client, conn); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	if p.remote != client {
		t.Error("p.remote was not set to the connected client")
	}
	if p.remoteConn != conn {
		t.Errorf("p.remoteConn = %+v, want %+v", p.remoteConn, conn)
	}
	if p.path != "/home/tester" {
		t.Errorf("p.path = %q, want the remote Root()", p.path)
	}
	if len(p.history) != 1 {
		t.Errorf("len(p.history) = %d, want 1 (connecting must reset history, not append to the local one)", len(p.history))
	}
}

func TestConnectRemoteClosesAPreviousConnectionFirst(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}

	first := &fakeRemoteClient{root: "/", entries: map[string][]fsops.Entry{"/": nil}}
	if err := p.connectRemote(first, remotefs.Connection{Host: "a"}); err != nil {
		t.Fatalf("connectRemote (first): %v", err)
	}

	second := &fakeRemoteClient{root: "/", entries: map[string][]fsops.Entry{"/": nil}}
	if err := p.connectRemote(second, remotefs.Connection{Host: "b"}); err != nil {
		t.Fatalf("connectRemote (second): %v", err)
	}

	if !first.closed {
		t.Error("the first connection was never closed when a second one replaced it")
	}
	if second.closed {
		t.Error("the second, now-active connection was closed too")
	}
}

func TestDisconnectRemoteClosesTheClientAndReturnsToLocalHome(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	client := &fakeRemoteClient{root: "/", entries: map[string][]fsops.Entry{"/": nil}}
	if err := p.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	if err := p.disconnectRemote(); err != nil {
		t.Fatalf("disconnectRemote: %v", err)
	}
	if !client.closed {
		t.Error("disconnectRemote did not close the remote client")
	}
	if p.remote != nil {
		t.Error("p.remote is still set after disconnectRemote")
	}
	if p.remoteConn != (remotefs.Connection{}) {
		t.Errorf("p.remoteConn = %+v, want the zero value after disconnecting", p.remoteConn)
	}
}

func TestDisconnectRemoteOnAnUnconnectedPanelIsANoOp(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	before := p.path
	if err := p.disconnectRemote(); err != nil {
		t.Fatalf("disconnectRemote: %v", err)
	}
	if p.path != before {
		t.Errorf("p.path changed from %q to %q after disconnecting an already-local panel", before, p.path)
	}
}

func TestLoadUsesTheRemoteClientWhenConnected(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	client := &fakeRemoteClient{root: "/remote", entries: map[string][]fsops.Entry{
		"/remote": {{Name: "a.txt", Type: fsops.TypeFile}, {Name: "sub", Type: fsops.TypeDir, IsDir: true}},
	}}
	if err := p.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	var names []string
	for row := 1; row < p.table.GetRowCount(); row++ { // row 0 is the ".."/root row
		if ref, ok := p.rowRef(row); ok {
			names = append(names, ref.name)
		}
	}
	if strings.Join(names, ",") != "a.txt,sub" {
		t.Errorf("listed names = %v, want the fake remote client's own entries, not the local directory's", names)
	}
}

func TestActionHomeUsesTheRemoteRootWhenConnected(t *testing.T) {
	dir := fixtureDir(t)
	p, err := NewPanel(tview.NewApplication(), dir, config.DefaultTheme().Resolve(), config.DefaultSettings())
	if err != nil {
		t.Fatalf("NewPanel: %v", err)
	}
	client := &fakeRemoteClient{root: "/home/tester", entries: map[string][]fsops.Entry{
		"/home/tester": nil,
		"/other":       nil,
	}}
	if err := p.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	if err := p.navigate("/other"); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	p.runHeaderAction(headerSpan{action: actionHome})
	if p.path != "/home/tester" {
		t.Errorf("p.path = %q after the Home button, want the remote Root() (%q), not a local directory", p.path, "/home/tester")
	}
}

func TestBuildHeaderSpansColorsTheConnectionButtonByState(t *testing.T) {
	theme := config.DefaultTheme().Resolve()

	// Pinned to the glow's own dimmest instant (see
	// TestConnectionGlowColorAtItsDimmestPointEqualsTheBaseColor) so the
	// connected color is deterministically exactly theme.EntryExecutable,
	// not whatever the wall clock happens to be mid-breath right now.
	old := connectionGlowNow
	connectionGlowNow = func() time.Time { return time.UnixMilli(2250) }
	defer func() { connectionGlowNow = old }()

	localText, _ := buildHeaderSpans("/", theme, false)
	connectedText, _ := buildHeaderSpans("/", theme, true)

	mutedTag := colorTag(theme.MutedTextColor)
	connectedTag := colorTag(theme.EntryExecutable)

	if !strings.Contains(localText, mutedTag) {
		t.Errorf("local header text %q does not contain the muted color tag %q", localText, mutedTag)
	}
	if !strings.Contains(connectedText, connectedTag) {
		t.Errorf("connected header text %q does not contain the connected color tag %q", connectedText, connectedTag)
	}
	if strings.Contains(connectedText, mutedTag) {
		t.Errorf("connected header text %q still contains the muted color tag", connectedText)
	}
}

// TestRefreshActivePanelHeaderGlowUpdatesTheConnectedHeaderColor pins
// StartClock's own once-a-second call to this: the header's own text
// must actually change as the glow's phase advances, not just get
// rewritten with the same color every tick.
func TestRefreshActivePanelHeaderGlowUpdatesTheConnectedHeaderColor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	client := fakeConnectedClient()
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}

	old := connectionGlowNow
	defer func() { connectionGlowNow = old }()

	connectionGlowNow = func() time.Time { return time.UnixMilli(2250) } // dimmest
	r.refreshActivePanelHeaderGlow()
	dim := r.panel.header.GetText(false)

	connectionGlowNow = func() time.Time { return time.UnixMilli(750) } // brightest
	r.refreshActivePanelHeaderGlow()
	bright := r.panel.header.GetText(false)

	if dim == bright {
		t.Error("refreshActivePanelHeaderGlow produced identical header text at the glow's dimmest and brightest points")
	}
}

// TestRefreshActivePanelHeaderGlowDoesNothingForALocalPanel confirms
// the no-op guard: a plain local panel's header must never be
// rewritten by this once-a-second call at all.
func TestRefreshActivePanelHeaderGlowDoesNothingForALocalPanel(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	before := r.panel.header.GetText(true)

	r.refreshActivePanelHeaderGlow()

	if got := r.panel.header.GetText(true); got != before {
		t.Errorf("header text changed for a local panel: %q -> %q", before, got)
	}
}
