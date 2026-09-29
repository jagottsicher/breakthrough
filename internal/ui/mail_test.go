package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// isolateMailDetection fakes mailDetect/mailInstalled to report exactly
// installed as found — the same isolation shape isolateSessionsList
// already establishes for listMultiplexSessions, so a test never
// depends on what's actually on $PATH on the machine running it.
func isolateMailDetection(t *testing.T, installed ...string) {
	t.Helper()
	origDetect, origInstalled := mailDetect, mailInstalled
	t.Cleanup(func() { mailDetect, mailInstalled = origDetect, origInstalled })

	set := make(map[string]bool, len(installed))
	for _, c := range installed {
		set[c] = true
	}
	mailDetect = func() []string {
		var found []string
		for _, c := range installed {
			found = append(found, c)
		}
		return found
	}
	mailInstalled = func(client string) bool { return set[client] }
}

func newTestRootForMail(t *testing.T) *Root {
	t.Helper()
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	return r
}

func TestResolveMailClientPrefersTheConfiguredOneWhenStillInstalled(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t, "neomutt", "aerc")
	r.settings.MailClient = "aerc"

	if got := r.resolveMailClient(); got != "aerc" {
		t.Errorf("resolveMailClient() = %q, want \"aerc\" (the configured choice)", got)
	}
}

func TestResolveMailClientFallsBackWhenConfiguredOneIsNoLongerInstalled(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t, "neomutt", "mail")
	r.settings.MailClient = "aerc" // configured, but not in the installed set below

	if got := r.resolveMailClient(); got != "neomutt" {
		t.Errorf("resolveMailClient() = %q, want \"neomutt\" (auto-detected fallback)", got)
	}
}

func TestResolveMailClientAutoDetectsWhenNothingConfigured(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t, "himalaya", "mailx")
	r.settings.MailClient = ""

	if got := r.resolveMailClient(); got != "himalaya" {
		t.Errorf("resolveMailClient() = %q, want \"himalaya\" (mailDetect's own first result)", got)
	}
}

func TestResolveMailClientEmptyWhenNoneInstalled(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t)
	r.settings.MailClient = ""

	if got := r.resolveMailClient(); got != "" {
		t.Errorf("resolveMailClient() = %q, want \"\"", got)
	}
}

func TestOpenMailShowsATransientNoticeWhenNoneInstalled(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t)

	r.openMail()

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the transient notice", r.activePage)
	}
	// The overlay word-wraps at a narrow width, so check the
	// whitespace-normalized text rather than one literal substring.
	got := strings.Join(strings.Fields(r.errorView.GetText(true)), " ")
	if !strings.Contains(got, "no mail client found") {
		t.Errorf("notice text = %q, want it to say no client was found", got)
	}
}

// TestOpenMailWithAnInstalledClientDoesNotError pins that reaching a
// real, resolved client runs cleanly — r.app.Suspend is a verified
// no-op without a real terminal (Application.Run was never called —
// see tview's own Application.Suspend, which bails out while its
// screen is still nil), the same acknowledged limitation Sessions' own
// TestActivateSessionsCellNameColumnAttachesWithoutError already notes,
// so this exercises openMail's own outcome handling, never a real
// mail-client invocation.
func TestOpenMailWithAnInstalledClientDoesNotError(t *testing.T) {
	r := newTestRootForMail(t)
	isolateMailDetection(t, "neomutt")

	r.openMail()

	if r.activePage == errorPage {
		t.Errorf("openMail reported an error: %q", r.errorView.GetText(true))
	}
}
