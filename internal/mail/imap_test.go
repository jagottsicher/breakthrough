package mail

import (
	"errors"
	"net"
	"testing"

	imap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
)

// fakeIMAPSession implements imapserver.Session with just enough to
// exercise IMAPSource.UnreadCount's own two real IMAP commands —
// Login and Status — against a real client/server wire exchange
// rather than a hand-rolled mock of the client API itself. Every
// other method the interface requires but UnreadCount never reaches
// returns an error outright: a real, unexpected call to one of those
// is a test bug worth failing loudly on, not silently no-op'd.
type fakeIMAPSession struct {
	wantUser, wantPass string
	unseen             map[string]uint32 // mailbox -> unseen count
}

var errFakeIMAPUnsupported = errors.New("fakeIMAPSession: not supported by this fake")

func (s *fakeIMAPSession) Close() error { return nil }

func (s *fakeIMAPSession) Login(username, password string) error {
	if username != s.wantUser || password != s.wantPass {
		return imapserver.ErrAuthFailed
	}
	return nil
}

func (s *fakeIMAPSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	n, ok := s.unseen[mailbox]
	if !ok {
		return nil, errors.New("fakeIMAPSession: no such mailbox")
	}
	data := &imap.StatusData{Mailbox: mailbox}
	if options.NumUnseen {
		data.NumUnseen = &n
	}
	return data, nil
}

func (s *fakeIMAPSession) Select(string, *imap.SelectOptions) (*imap.SelectData, error) {
	return nil, errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Create(string, *imap.CreateOptions) error { return errFakeIMAPUnsupported }
func (s *fakeIMAPSession) Delete(string) error                      { return errFakeIMAPUnsupported }
func (s *fakeIMAPSession) Rename(string, string, *imap.RenameOptions) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Subscribe(string) error   { return errFakeIMAPUnsupported }
func (s *fakeIMAPSession) Unsubscribe(string) error { return errFakeIMAPUnsupported }
func (s *fakeIMAPSession) List(*imapserver.ListWriter, string, []string, *imap.ListOptions) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Append(string, imap.LiteralReader, *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Poll(*imapserver.UpdateWriter, bool) error { return nil }
func (s *fakeIMAPSession) Idle(*imapserver.UpdateWriter, <-chan struct{}) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Unselect() error { return errFakeIMAPUnsupported }
func (s *fakeIMAPSession) Expunge(*imapserver.ExpungeWriter, *imap.UIDSet) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Search(imapserver.NumKind, *imap.SearchCriteria, *imap.SearchOptions) (*imap.SearchData, error) {
	return nil, errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Fetch(*imapserver.FetchWriter, imap.NumSet, *imap.FetchOptions) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Store(*imapserver.FetchWriter, imap.NumSet, *imap.StoreFlags, *imap.StoreOptions) error {
	return errFakeIMAPUnsupported
}
func (s *fakeIMAPSession) Copy(imap.NumSet, string) (*imap.CopyData, error) {
	return nil, errFakeIMAPUnsupported
}

// discardIMAPServerLog silences imapserver's own default stderr
// logging — UnreadCount's own deliberate "Close immediately after
// Status, no Logout.Wait()" shape (see its own doc comment) always
// makes the server log a harmless "connection reset by peer" once the
// client hangs up without finishing the exchange politely; real
// production behavior, not a test failure, so there's nothing for a
// human running `go test -v` to actually act on here.
type discardIMAPServerLog struct{}

func (discardIMAPServerLog) Printf(string, ...interface{}) {}

// startFakeIMAPServer starts a real imapserver.Server, listening on
// 127.0.0.1 on a random free port, authenticating exactly one
// user/password and serving STATUS for the given unseen map — returns
// its address and a cleanup func (t.Cleanup is used instead, so
// callers just need the address).
func startFakeIMAPServer(t *testing.T, user, pass string, unseen map[string]uint32) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	server := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &fakeIMAPSession{wantUser: user, wantPass: pass, unseen: unseen}, nil, nil
		},
		InsecureAuth: true, // plaintext test server — see imapDial's own test override, no TLS involved
		Logger:       discardIMAPServerLog{},
	})
	t.Cleanup(func() { _ = server.Close() }) // test teardown; nothing to assert on a close failure here

	go func() {
		_ = server.Serve(ln)
	}()

	return ln.Addr().String()
}

// withFakeIMAPDial swaps imapDial, for the duration of the test, to
// connect to addr over plain TCP (imapclient.DialInsecure) instead of
// real TLS against cfg.Host/cfg.port() — the one seam IMAPSource
// exposes specifically so a test can exercise the real Login/Status
// wire protocol without a real certificate.
func withFakeIMAPDial(t *testing.T, addr string) {
	t.Helper()
	original := imapDial
	imapDial = func(IMAPConfig) (*imapclient.Client, error) {
		return imapclient.DialInsecure(addr, nil)
	}
	t.Cleanup(func() { imapDial = original })
}

func TestIMAPSourceUnreadCountReturnsTheServersOwnCount(t *testing.T) {
	addr := startFakeIMAPServer(t, "alice", "s3cret", map[string]uint32{"INBOX": 7})
	withFakeIMAPDial(t, addr)
	writeNetrc(t, "machine imap.example.com login alice password s3cret")

	src := NewIMAPSource(IMAPConfig{Host: "imap.example.com"})
	n, err := src.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if n != 7 {
		t.Errorf("UnreadCount = %d, want 7", n)
	}
}

func TestIMAPSourceUnreadCountUsesConfiguredMailbox(t *testing.T) {
	addr := startFakeIMAPServer(t, "alice", "s3cret", map[string]uint32{"Work": 3, "INBOX": 99})
	withFakeIMAPDial(t, addr)
	writeNetrc(t, "machine imap.example.com login alice password s3cret")

	src := NewIMAPSource(IMAPConfig{Host: "imap.example.com", Mailbox: "Work"})
	n, err := src.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if n != 3 {
		t.Errorf("UnreadCount = %d, want 3 (the configured mailbox, not INBOX)", n)
	}
}

func TestIMAPSourceUnreadCountFailsOnWrongNetrcPassword(t *testing.T) {
	addr := startFakeIMAPServer(t, "alice", "s3cret", map[string]uint32{"INBOX": 7})
	withFakeIMAPDial(t, addr)
	writeNetrc(t, "machine imap.example.com login alice password wrong-password")

	src := NewIMAPSource(IMAPConfig{Host: "imap.example.com"})
	if _, err := src.UnreadCount(); err == nil {
		t.Error("UnreadCount should have failed with the wrong netrc password")
	}
}

func TestIMAPSourceUnreadCountFailsWithNoNetrcEntry(t *testing.T) {
	addr := startFakeIMAPServer(t, "alice", "s3cret", map[string]uint32{"INBOX": 7})
	withFakeIMAPDial(t, addr)
	writeNetrc(t, "machine other.example.com login bob password whatever")

	src := NewIMAPSource(IMAPConfig{Host: "imap.example.com"})
	if _, err := src.UnreadCount(); err == nil {
		t.Error("UnreadCount should have failed with no matching netrc entry")
	}
}

func TestIMAPConfigPortDefaults(t *testing.T) {
	if got := (IMAPConfig{}).port(); got != IMAPDefaultPort {
		t.Errorf("default port = %d, want %d", got, IMAPDefaultPort)
	}
	if got := (IMAPConfig{StartTLS: true}).port(); got != IMAPDefaultStartTLSPort {
		t.Errorf("StartTLS default port = %d, want %d", got, IMAPDefaultStartTLSPort)
	}
	if got := (IMAPConfig{Port: 2525}).port(); got != 2525 {
		t.Errorf("explicit port = %d, want 2525 preserved", got)
	}
}

func TestIMAPConfigMailboxDefaults(t *testing.T) {
	if got := (IMAPConfig{}).mailbox(); got != IMAPDefaultMailbox {
		t.Errorf("default mailbox = %q, want %q", got, IMAPDefaultMailbox)
	}
	if got := (IMAPConfig{Mailbox: "Work"}).mailbox(); got != "Work" {
		t.Errorf("explicit mailbox = %q, want %q preserved", got, "Work")
	}
}

func TestIMAPSourceNameReturnsHost(t *testing.T) {
	if got := NewIMAPSource(IMAPConfig{Host: "imap.example.com"}).Name(); got != "imap.example.com" {
		t.Errorf("Name() = %q, want %q", got, "imap.example.com")
	}
}
