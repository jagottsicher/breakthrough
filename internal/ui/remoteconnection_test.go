package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

// fakeRemoteClient is an in-memory, stateful remotefs.Client double —
// Panel's own remote-wiring tests need something to attach as p.remote
// that actually behaves like a small filesystem (rename/remove/chmod/
// read/write all really mutate it), not just a stub that returns
// canned errors; exercising a real SFTP wire round trip belongs to
// internal/remotefs's own test suite (see its testserver_test.go), not
// here.
//
// entries maps a directory path to its own children, Lstat-shaped —
// the same shape ListDir itself already returns. content holds file
// bytes by full path, for Open/Create. Both are plain maps, not
// goroutine-safe: fine here since every test using this runs on a
// single goroutine, the same assumption panel_test.go's own fixtures
// already make.
type fakeRemoteClient struct {
	root    string
	entries map[string][]fsops.Entry
	content map[string][]byte
	closed  bool
}

var _ remotefs.Client = (*fakeRemoteClient)(nil)

func (f *fakeRemoteClient) Root() string { return f.root }

// ListDir sorts its own result the same way the real SFTPClient.ListDir
// does (directories first, then case-insensitive name — see its own
// doc comment on why) rather than just returning entries in whatever
// order they were inserted into this fake: a test relying on a
// specific row's own index (focusRow, CurrentRowPath, ...) should see
// the exact same order the real client would actually produce.
func (f *fakeRemoteClient) ListDir(dir string) ([]fsops.Entry, error) {
	children, ok := f.entries[dir]
	if !ok {
		return nil, fmt.Errorf("fakeRemoteClient: no such directory: %s", dir)
	}
	sorted := append([]fsops.Entry(nil), children...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].IsDir != sorted[j].IsDir {
			return sorted[i].IsDir
		}
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	return sorted, nil
}

func (f *fakeRemoteClient) findEntry(p string) (dir string, index int, ok bool) {
	dir = path.Dir(p)
	name := path.Base(p)
	for i, e := range f.entries[dir] {
		if e.Name == name {
			return dir, i, true
		}
	}
	return dir, -1, false
}

// Lstat and Stat are identical here: this fake never models a symlink
// (see fakeSymlinkEntry, used by tests that specifically need one),
// so there's nothing for a real Stat's own symlink-following to differ
// on.
func (f *fakeRemoteClient) Lstat(p string) (fsops.Entry, error) { return f.Stat(p) }

func (f *fakeRemoteClient) Stat(p string) (fsops.Entry, error) {
	dir, i, ok := f.findEntry(p)
	if !ok {
		return fsops.Entry{}, fmt.Errorf("fakeRemoteClient: no such file: %s", p)
	}
	return f.entries[dir][i], nil
}

func (f *fakeRemoteClient) Open(p string) (io.ReadCloser, error) {
	data, ok := f.content[p]
	if !ok {
		return nil, fmt.Errorf("fakeRemoteClient: no such file: %s", p)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fakeRemoteWriteCloser buffers Write calls and commits them to the
// owning fake's own content map only on Close — the same "content
// isn't real until the writer is closed" contract a real SFTP upload
// has, since a caller can Write in several chunks before ever calling
// Close.
type fakeRemoteWriteCloser struct {
	client *fakeRemoteClient
	path   string
	buf    bytes.Buffer
}

func (w *fakeRemoteWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *fakeRemoteWriteCloser) Close() error {
	if w.client.content == nil {
		w.client.content = map[string][]byte{}
	}
	w.client.content[w.path] = append([]byte(nil), w.buf.Bytes()...)
	if _, _, ok := w.client.findEntry(w.path); !ok {
		dir := path.Dir(w.path)
		w.client.entries[dir] = append(w.client.entries[dir], fsops.Entry{Name: path.Base(w.path), Type: fsops.TypeFile})
	}
	return nil
}

func (f *fakeRemoteClient) Create(p string) (io.WriteCloser, error) {
	return &fakeRemoteWriteCloser{client: f, path: p}, nil
}

func (f *fakeRemoteClient) Mkdir(p string) error {
	if _, ok := f.entries[p]; ok {
		return fmt.Errorf("fakeRemoteClient: already exists: %s", p)
	}
	dir := path.Dir(p)
	f.entries[dir] = append(f.entries[dir], fsops.Entry{Name: path.Base(p), Type: fsops.TypeDir, IsDir: true})
	f.entries[p] = nil // an empty, but now-listable, directory
	return nil
}

func (f *fakeRemoteClient) Remove(p string) error {
	dir, i, ok := f.findEntry(p)
	if !ok {
		return fmt.Errorf("fakeRemoteClient: no such file: %s", p)
	}
	f.entries[dir] = append(f.entries[dir][:i], f.entries[dir][i+1:]...)
	delete(f.content, p)
	return nil
}

func (f *fakeRemoteClient) RemoveDirectory(p string) error {
	if children := f.entries[p]; len(children) > 0 {
		return fmt.Errorf("fakeRemoteClient: directory not empty: %s", p)
	}
	dir, i, ok := f.findEntry(p)
	if !ok {
		return fmt.Errorf("fakeRemoteClient: no such directory: %s", p)
	}
	f.entries[dir] = append(f.entries[dir][:i], f.entries[dir][i+1:]...)
	delete(f.entries, p)
	return nil
}

func (f *fakeRemoteClient) Rename(oldPath, newPath string) error {
	if _, _, ok := f.findEntry(newPath); ok {
		return fmt.Errorf("fakeRemoteClient: already exists: %s", newPath)
	}
	dir, i, ok := f.findEntry(oldPath)
	if !ok {
		return fmt.Errorf("fakeRemoteClient: no such file: %s", oldPath)
	}
	entry := f.entries[dir][i]
	entry.Name = path.Base(newPath)
	f.entries[dir] = append(f.entries[dir][:i], f.entries[dir][i+1:]...)
	newDir := path.Dir(newPath)
	f.entries[newDir] = append(f.entries[newDir], entry)
	if data, ok := f.content[oldPath]; ok {
		f.content[newPath] = data
		delete(f.content, oldPath)
	}
	if children, ok := f.entries[oldPath]; ok {
		f.entries[newPath] = children
		delete(f.entries, oldPath)
	}
	return nil
}

func (f *fakeRemoteClient) Chmod(p string, mode os.FileMode) error {
	dir, i, ok := f.findEntry(p)
	if !ok {
		return fmt.Errorf("fakeRemoteClient: no such file: %s", p)
	}
	f.entries[dir][i].Mode = mode
	return nil
}

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
	if strings.Join(names, ",") != "sub,a.txt" { // directories sort before files (see ListDir's own doc comment)
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

	// Pinned to one of the glow's own "at rest" instants (see
	// TestConnectionGlowColorAtRestEqualsTheBaseColor) so the connected
	// color is deterministically exactly theme.EntryExecutable, not
	// whatever the wall clock happens to be mid-breath right now.
	old := connectionGlowNow
	connectionGlowNow = func() time.Time { return time.UnixMilli(0) }
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
