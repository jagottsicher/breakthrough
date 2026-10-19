package ui

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/rclone"
)

func cloudRemoteBackendByType(t *testing.T, backendType string) cloudRemoteBackend {
	t.Helper()
	for _, b := range cloudRemoteBackends {
		if b.backendType == backendType {
			return b
		}
	}
	t.Fatalf("no cloudRemoteBackends entry for %q", backendType)
	return cloudRemoteBackend{}
}

// TestCloudRemoteParamsBuildsS3WithProvider pins the one backend whose
// sub-option (provider) also covers Aliyun OSS (value "Alibaba", per
// a real `rclone help backend s3` check, not guessed) — all fields
// filled, including the two optional ones.
func TestCloudRemoteParamsBuildsS3WithProvider(t *testing.T) {
	backend := cloudRemoteBackendByType(t, "s3")
	// subOptions[1] is Alibaba in cloudRemoteBackends' own declared
	// order — if that ever moves, this test's own failure is the
	// signal, not a silent pass.
	if backend.subOptions[1].value != "Alibaba" {
		t.Fatalf("subOptions[1] = %+v, want value %q (Aliyun OSS)", backend.subOptions[1], "Alibaba")
	}

	params, missing := cloudRemoteParams(backend, 1, []string{"AKIAEXAMPLE", "secretvalue", "oss-endpoint.example.com", "us-east-1"})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	want := []string{
		"provider=Alibaba",
		"access_key_id=AKIAEXAMPLE",
		"secret_access_key=secretvalue",
		"endpoint=oss-endpoint.example.com",
		"region=us-east-1",
	}
	if strings.Join(params, "|") != strings.Join(want, "|") {
		t.Errorf("params = %v, want %v", params, want)
	}
}

// TestCloudRemoteParamsSkipsEmptyOptionalFields pins that an optional
// field left blank is simply omitted — never sent as "key=" — while a
// required one left blank is reported instead of silently omitted,
// per the real `rclone config create` behavior confirmed separately
// (a required field missing creates a half-configured remote rather
// than refusing).
func TestCloudRemoteParamsSkipsEmptyOptionalFields(t *testing.T) {
	backend := cloudRemoteBackendByType(t, "s3")
	params, missing := cloudRemoteParams(backend, 0, []string{"AKIAEXAMPLE", "secretvalue", "", ""})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none (endpoint/region are optional)", missing)
	}
	for _, p := range params {
		if strings.HasPrefix(p, "endpoint=") || strings.HasPrefix(p, "region=") {
			t.Errorf("params = %v, should never include an empty optional field", params)
		}
	}
}

// TestCloudRemoteParamsReportsMissingRequiredFields pins that required
// fields left blank are reported by label, and no params are built at
// all once anything is missing — the caller (runCreateCloudRemote)
// must never reach CreateStaticRemote with an incomplete field set.
func TestCloudRemoteParamsReportsMissingRequiredFields(t *testing.T) {
	backend := cloudRemoteBackendByType(t, "ftp")
	_, missing := cloudRemoteParams(backend, -1, []string{"", "", "someuser", ""})
	if len(missing) != 1 || missing[0] != "Host" {
		t.Errorf("missing = %v, want [\"Host\"]", missing)
	}
}

// TestCloudRemoteParamsBuildsWebdav pins the vendor sub-option and
// that User/Password stay optional (some WebDAV servers need no
// auth at all).
func TestCloudRemoteParamsBuildsWebdav(t *testing.T) {
	backend := cloudRemoteBackendByType(t, "webdav")
	params, missing := cloudRemoteParams(backend, 1, []string{"https://cloud.example.com/remote.php/webdav/", "", ""})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	want := []string{"vendor=nextcloud", "url=https://cloud.example.com/remote.php/webdav/"}
	if strings.Join(params, "|") != strings.Join(want, "|") {
		t.Errorf("params = %v, want %v", params, want)
	}
}

// TestCloudRemoteParamsBuildsAzureBlob pins the simplest backend here
// — no sub-option, both its own fields required.
func TestCloudRemoteParamsBuildsAzureBlob(t *testing.T) {
	backend := cloudRemoteBackendByType(t, "azureblob")
	params, missing := cloudRemoteParams(backend, -1, []string{"myaccount", "mykey"})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	want := []string{"account=myaccount", "key=mykey"}
	if strings.Join(params, "|") != strings.Join(want, "|") {
		t.Errorf("params = %v, want %v", params, want)
	}
}

// TestCloudRemoteNameAcceptanceFuncMatchesRclonesOwnRule pins the
// exact character set a real `rclone config create` with an invalid
// name reported in its own error message, not guessed.
func TestCloudRemoteNameAcceptanceFuncMatchesRclonesOwnRule(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"my-remote_1", true},
		{"my.remote+tag@host name", true},
		{"bad:name", false},
		{"bad/name", false},
		{"bad!name", false},
		{strings.Repeat("a", cloudRemoteNameMaxLength), true},
		{strings.Repeat("a", cloudRemoteNameMaxLength+1), false},
	}
	for _, c := range cases {
		if got := cloudRemoteNameAcceptanceFunc(c.text, 0); got != c.want {
			t.Errorf("cloudRemoteNameAcceptanceFunc(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestValidCloudRemoteName pins the other half of rclone's own rule
// (confirmed via the same real error message): must not start with
// "-", must not be empty. The "doesn't end with a space" half is moot
// here — runCreateCloudRemote always calls this against an already
// strings.TrimSpace'd name.
func TestValidCloudRemoteName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"", false},
		{"-leading-hyphen", false},
		{"my-remote", true},
		{"my.remote_1", true},
	}
	for _, c := range cases {
		if got := validCloudRemoteName(c.name); got != c.want {
			t.Errorf("validCloudRemoteName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestCloudRemoteDialogFormHeightAccountsForEveryRow pins the exact
// row-counting arithmetic (2 border-padding rows + one per field +
// one gap between each pair) against the two backends with the most
// and fewest fields — a wrong height here silently clips the last
// field or button row, the same real regression class
// newConnectLayout's own doc comment already warns about for a plain
// Form.
func TestCloudRemoteDialogFormHeightAccountsForEveryRow(t *testing.T) {
	s3 := cloudRemoteBackendByType(t, "s3") // backend type + name + provider + 4 fields = 7 rows
	if got, want := cloudRemoteDialogFormHeight(s3), 2+7+6; got != want {
		t.Errorf("cloudRemoteDialogFormHeight(s3) = %d, want %d", got, want)
	}
	azure := cloudRemoteBackendByType(t, "azureblob") // backend type + name + 2 fields = 4 rows, no sub-option
	if got, want := cloudRemoteDialogFormHeight(azure), 2+4+3; got != want {
		t.Errorf("cloudRemoteDialogFormHeight(azureblob) = %d, want %d", got, want)
	}
}

// TestObscureTestParamsObscuresOnlyFlaggedFields pins that only a
// field flagged obscure:true (WebDAV/FTP's own "pass") gets re-
// obscured for TestRemote's own on-the-fly syntax — S3's
// secret_access_key must stay exactly as typed, confirmed separately
// that rclone itself never obscures that one either.
func TestObscureTestParamsObscuresOnlyFlaggedFields(t *testing.T) {
	requireRclone(t)

	webdav := cloudRemoteBackendByType(t, "webdav")
	got, err := obscureTestParams(webdav, []string{"url=https://example.com/dav", "user=jens", "pass=secret123"})
	if err != nil {
		t.Fatalf("obscureTestParams: %v", err)
	}
	if got[0] != "url=https://example.com/dav" || got[1] != "user=jens" {
		t.Errorf("non-obscure fields changed: %v", got)
	}
	if got[2] == "pass=secret123" {
		t.Error("pass should have been obscured, was left as plain text")
	}
	revealed, err := exec.Command("rclone", "reveal", strings.TrimPrefix(got[2], "pass=")).Output()
	if err != nil {
		t.Fatalf("rclone reveal: %v", err)
	}
	if got := strings.TrimSpace(string(revealed)); got != "secret123" {
		t.Errorf("rclone reveal(obscured pass) = %q, want %q", got, "secret123")
	}

	s3 := cloudRemoteBackendByType(t, "s3")
	got, err = obscureTestParams(s3, []string{"access_key_id=AKIA", "secret_access_key=plainsecret"})
	if err != nil {
		t.Fatalf("obscureTestParams: %v", err)
	}
	if got[1] != "secret_access_key=plainsecret" {
		t.Errorf("secret_access_key should be left exactly as typed, got %v", got)
	}
}

// TestRunTestCloudRemoteShowsTestingImmediately pins
// runTestCloudRemote's own synchronous half: it sets the "Testing…"
// status and returns right away, the real rclone.TestRemote call
// happening on its own background goroutine (see safeGo) — never
// blocking the UI thread, the same non-blocking-background-work rule
// toggleCloudMount already follows for its own real Mount/Unmount
// calls.
//
// Deliberately doesn't wait for or assert the final "✓ Connection OK"/
// "✕ ..." status QueueUpdateDraw hands back: that hop never fires in
// a test lacking a live Application.Run() event loop to drain it (see
// pasteconflict_test.go's own identical, already-documented
// limitation) — rclone.TestRemote's own real round trip is already
// covered directly, without any of this UI wrapping, by
// TestTestRemoteRealRoundTrip one package down.
func TestRunTestCloudRemoteShowsTestingImmediately(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudRemoteDialog()

	r.runTestCloudRemote()

	if got := r.cloudRemoteDialogStatus.GetText(true); got != "Testing…" {
		t.Errorf("status right after calling runTestCloudRemote = %q, want %q", got, "Testing…")
	}
}

// TestRunSaveCloudRemoteLenientPersistsIncompleteRemote pins OK's own
// deliberate leniency: a remote missing its own required fields still
// gets created — the explicit "I know it's incomplete" action the
// user asked for, as opposed to Create's own strict refusal (see
// runCreateCloudRemote's own test coverage in cloudmountdialog_test.go
// — no direct equivalent test for Create's own refusal exists in this
// file, since that behavior is already pinned by
// TestCreateStaticRemoteRefusesAnExistingName one package down; this
// test is about the new OK path specifically).
func TestRunSaveCloudRemoteLenientPersistsIncompleteRemote(t *testing.T) {
	requireRclone(t)
	r := newTestRootForCloudMount(t)
	r.openCloudRemoteDialog()
	r.cloudRemoteDialogNameField.SetText("incomplete-s3")
	// Leave every field empty — S3's own access_key_id/secret_access_key
	// are both required, per cloudRemoteBackends' own table.

	r.runSaveCloudRemoteLenient()

	remotes, err := rclone.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	found := false
	for _, rem := range remotes {
		if rem.Name == "incomplete-s3" {
			found = true
		}
	}
	if !found {
		t.Error("OK should have persisted the remote despite missing required fields")
	}
}
