package ui

import (
	"strings"
	"testing"
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
