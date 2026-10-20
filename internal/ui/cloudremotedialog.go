package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/rclone"
)

// The "New cloud remote" dialog — creates a brand-new rclone remote
// with static credentials via rclone.CreateStaticRemote, reached by
// picking "+ Create new remote…" from the "New cloud mount" dialog's
// own Remote dropdown (see cloudMountDialogCreateNewOption). On
// success, hands back to that same dialog with the new remote
// preselected — creating a remote and mounting it are still two
// separate, deliberate steps (see runAddCloudMount's own doc comment
// on why), just no longer requiring a detour through a real terminal
// and `rclone config create` by hand in between.
//
// Scoped to the four backend types with static, non-interactive
// credentials — S3 (which, via its own "provider" option, also covers
// every S3 clone including Aliyun/Alibaba OSS, MinIO, Wasabi, Ceph,
// ...), WebDAV (covers Nextcloud, MagentaCloud, ownCloud, ...), FTP,
// and Azure Blob Storage. OAuth backends (OneDrive, Dropbox, Google
// Drive) need a browser-based consent step no non-interactive form
// can give them — out of scope here, same as rclone.CreateStaticRemote
// itself already documents.

const cloudRemoteDialogPage = "cloud-remote-dialog"

// cloudRemoteNameMaxLength mirrors connectIdentifierMaxLength's own
// reasoning: long enough to tell several remotes apart, short enough
// to never dominate the merged list's own User column.
const cloudRemoteNameMaxLength = 32

// isCloudRemoteNameRune reports whether r is allowed in a remote name
// — confirmed against a real `rclone config create` run with an
// invalid name, not guessed: its own error message states exactly
// this set verbatim ("config name contains invalid characters - may
// only contain numbers, letters, `_`, `-`, `.`, `+`, `@` and space").
// The "must not start with '-' or space, must not end with space"
// half of that same rule is checked separately, once, on submit (see
// runCreateCloudRemote) — a live per-keystroke check can't express
// "the last character so far" without also rejecting a name that's
// simply not finished yet.
func isCloudRemoteNameRune(r rune) bool {
	return r == '_' || r == '-' || r == '.' || r == '+' || r == '@' || r == ' ' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func cloudRemoteNameAcceptanceFunc(textToCheck string, lastChar rune) bool {
	if len(textToCheck) > cloudRemoteNameMaxLength {
		return false
	}
	for _, r := range textToCheck {
		if !isCloudRemoteNameRune(r) {
			return false
		}
	}
	return true
}

// validCloudRemoteName reports whether name (already trimmed) is
// accepted by `rclone config create` as a remote name — the other
// half of isCloudRemoteNameRune's own rule, checked once on submit
// rather than per keystroke (see its own doc comment for why).
func validCloudRemoteName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "-")
}

// cloudRemoteFieldSpec describes one plain text form field for a
// given backend — its rclone Config name, its form label, a
// placeholder, whether it masks input (a real secret, not just
// something sensitive-looking), whether CreateStaticRemote is refused
// outright when it's left empty, and whether rclone.TestRemote's own
// on-the-fly connection string needs it pre-obscured. Declared as data
// rather than one branch of code per backend (see cloudRemoteBackends'
// own doc comment) — the four backends in scope here differ only in
// which fields they need and whether each one is required/obscure-
// needing, nothing about how a field itself behaves.
//
// obscure is deliberately its own flag, not just "masked" reused:
// masked is a display concern (every one of these happens to also be
// masked), obscure is about what rclone's own backend definition
// expects on the wire. S3's secret_access_key and Azure's key are
// masked but never obscured — confirmed empirically that
// `rclone config create` leaves them in plain text — while WebDAV/
// FTP's pass fields are both masked and must be obscured, confirmed
// the same way (`rclone reveal` round-trip, and TestRemote's own
// on-the-fly syntax outright failing to "decrypt" a plain one).
type cloudRemoteFieldSpec struct {
	key         string
	label       string
	placeholder string
	masked      bool
	required    bool
	obscure     bool
}

// cloudRemoteOption is one choice in a backend's own sub-option
// dropdown (S3's "provider", WebDAV's "vendor") — value is exactly
// what rclone's own Config expects; display is what the dropdown
// shows, which only differs from value for Alibaba (shown as "Alibaba
// (Aliyun OSS)" — the Aliyun name is what the user's own original
// request actually said, Alibaba is rclone's own internal name for
// the exact same provider, confirmed via `rclone help backend s3`).
type cloudRemoteOption struct {
	value   string
	display string
}

// cloudRemoteBackend describes one backend type this dialog can
// create a remote for: its rclone backend type, its own label in the
// top-level chooser, an optional sub-option dropdown (S3's provider,
// WebDAV's vendor — "" key means none), and its plain fields.
//
// A real `rclone config create --non-interactive` run with a required
// field left out (ftp with no host) was confirmed to silently create
// a half-configured, unusable remote rather than refusing — required
// is enforced here, client-side, before CreateStaticRemote is ever
// called, specifically because rclone itself won't.
type cloudRemoteBackend struct {
	backendType    string
	label          string
	subOptionKey   string
	subOptionLabel string
	subOptions     []cloudRemoteOption
	fields         []cloudRemoteFieldSpec
}

// cloudRemoteBackends is every backend this dialog offers, in the
// order the top-level dropdown lists them — the one table
// rebuildCloudRemoteDialogFields walks, rather than a branch per
// backend (see advisor review: "field sets belong in a table, not in
// four branches").
var cloudRemoteBackends = []cloudRemoteBackend{
	{
		backendType:    "s3",
		label:          "S3 (AWS, Aliyun OSS, MinIO, Wasabi, Ceph, ...)",
		subOptionKey:   "provider",
		subOptionLabel: "Provider",
		subOptions: []cloudRemoteOption{
			{"AWS", "AWS"},
			{"Alibaba", "Alibaba (Aliyun OSS)"},
			{"Minio", "MinIO"},
			{"Wasabi", "Wasabi"},
			{"Ceph", "Ceph"},
			{"DigitalOcean", "DigitalOcean Spaces"},
			{"Other", "Other S3-compatible"},
		},
		fields: []cloudRemoteFieldSpec{
			{"access_key_id", "Access Key ID", "", false, true, false},
			{"secret_access_key", "Secret Access Key", "", true, true, false},
			{"endpoint", "Endpoint", "required for every provider except AWS", false, false, false},
			{"region", "Region", "optional", false, false, false},
		},
	},
	{
		backendType:    "webdav",
		label:          "WebDAV (Nextcloud, MagentaCloud, ownCloud, ...)",
		subOptionKey:   "vendor",
		subOptionLabel: "Vendor",
		subOptions: []cloudRemoteOption{
			{"other", "Other"},
			{"nextcloud", "Nextcloud"},
			{"owncloud", "ownCloud"},
			{"sharepoint", "Sharepoint"},
			{"fastmail", "Fastmail Files"},
		},
		fields: []cloudRemoteFieldSpec{
			{"url", "URL", "https://example.com/remote.php/webdav/", false, true, false},
			{"user", "User", "optional", false, false, false},
			// obscure: true — confirmed by a real `rclone reveal`
			// round trip (config create) and a real failed on-the-fly
			// test against a plain value (TestRemote) alike: this is
			// a true rclone password field, not a plain credential
			// like S3's secret_access_key.
			{"pass", "Password", "optional", true, false, true},
		},
	},
	{
		backendType: "ftp",
		label:       "FTP",
		fields: []cloudRemoteFieldSpec{
			{"host", "Host", "", false, true, false},
			{"port", "Port", "default 21", false, false, false},
			{"user", "User", "optional", false, false, false},
			{"pass", "Password", "optional", true, false, true}, // obscure: true — see WebDAV's own identical field above
		},
	},
	{
		backendType: "azureblob",
		label:       "Azure Blob Storage",
		fields: []cloudRemoteFieldSpec{
			{"account", "Account", "", false, true, false},
			{"key", "Key", "", true, true, false},
		},
	},
}

// newCloudRemoteDialogForm builds the (initially empty) form once —
// just the backend-type DropDown and the Name field, which are the
// two things present no matter which backend is selected.
// rebuildCloudRemoteDialogFields fills in the rest, and runs again
// every time the backend selection changes.
func (r *Root) newCloudRemoteDialogForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.hideOverlay()
			return nil
		}
		return event
	})
	return f
}

// styleCloudRemoteDialogForm applies the current theme's own Form-
// level colors to cloudRemoteDialogForm — called from applyTheme (a
// live color-scheme switch) and from rebuildCloudRemoteDialogFields
// (which rebuilds the form from scratch on every backend-type change,
// via Form.Clear(true), wiping out whatever this already set), so the
// two can never drift out of sync with each other.
func (r *Root) styleCloudRemoteDialogForm() {
	r.cloudRemoteDialogForm.SetBackgroundColor(r.theme.SurfaceBackground)
	r.cloudRemoteDialogForm.SetLabelColor(r.theme.TextColor)
	r.cloudRemoteDialogForm.SetFieldBackgroundColor(r.theme.InputFocusedBackground)
	r.cloudRemoteDialogForm.SetFieldTextColor(r.theme.TextColor)
}

// newCloudRemoteDialogButtons mirrors newCloudMountDialogButtons'
// own Cancel/OK/Mount three-button shape, with one more: Cancel/Test/
// OK/Create, per the user's own explicit request.
//
//   - Test runs rclone.TestRemote against whatever the form currently
//     holds — real connectivity, nothing persisted — and reports the
//     result in cloudRemoteDialogStatus. Never blocks on missing
//     required fields itself: a real connection failure ("host
//     required") is more honest feedback than a pre-check duplicating
//     what the attempt itself will say anyway.
//   - OK persists the remote leniently: whatever's filled in, missing
//     required fields and all — the deliberate "I know it's
//     incomplete, save it anyway" action, per the user's own explicit
//     request after being told Create refuses outright (see
//     runSaveCloudRemoteLenient's own doc comment on why this is safe
//     now that it's an explicit choice rather than silent, unlike
//     plain `rclone config create` on its own).
//   - Create keeps the original, strict behavior: refuses if anything
//     required is missing.
func (r *Root) newCloudRemoteDialogButtons() *tview.Flex {
	r.cloudRemoteDialogCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.hideOverlay)
	r.cloudRemoteDialogTestBtn = tview.NewButton("Test").SetSelectedFunc(r.runTestCloudRemote)
	r.cloudRemoteDialogOKBtn = tview.NewButton("OK").SetSelectedFunc(r.runSaveCloudRemoteLenient)
	r.cloudRemoteDialogCreateBtn = tview.NewButton("Create").SetSelectedFunc(r.runCreateCloudRemote)
	r.cloudRemoteDialogCancelBtn.SetInputCapture(spaceAlsoActivates(r.hideOverlay))
	r.cloudRemoteDialogTestBtn.SetInputCapture(spaceAlsoActivates(r.runTestCloudRemote))
	r.cloudRemoteDialogOKBtn.SetInputCapture(spaceAlsoActivates(r.runSaveCloudRemoteLenient))
	r.cloudRemoteDialogCreateBtn.SetInputCapture(spaceAlsoActivates(r.runCreateCloudRemote))

	r.cloudRemoteDialogCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudRemoteDialogTestBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudRemoteDialogCreateBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.cloudRemoteDialogTestBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudRemoteDialogOKBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudRemoteDialogCancelBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.cloudRemoteDialogOKBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.cloudRemoteDialogCreateBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudRemoteDialogTestBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})
	r.cloudRemoteDialogCreateBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.cloudRemoteDialogForm.SetFocus(0)
			r.app.SetFocus(r.cloudRemoteDialogForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.cloudRemoteDialogOKBtn)
		case tcell.KeyEscape:
			r.hideOverlay()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.cloudRemoteDialogCancelBtn, 0, 1, false).
		AddItem(r.cloudRemoteDialogTestBtn, 0, 1, false).
		AddItem(r.cloudRemoteDialogOKBtn, 0, 1, false).
		AddItem(r.cloudRemoteDialogCreateBtn, 0, 1, false)
}

// newCloudRemoteDialogLayout mirrors newCloudMountDialogLayout's own
// shape — title bar, then form + buttons. The form's own height is
// recomputed on every rebuild (see resizeCloudRemoteDialog), not
// fixed here, since it genuinely changes per backend.
func (r *Root) newCloudRemoteDialogLayout() *tview.Flex {
	r.cloudRemoteDialogTitleBar = newPlainTitleBar("New cloud remote")
	r.cloudRemoteDialogStatus = tview.NewTextView().SetDynamicColors(true)
	r.cloudRemoteDialogContent = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.cloudRemoteDialogForm, 9, 0, true).
		AddItem(r.cloudRemoteDialogStatus, 1, 0, false).
		AddItem(r.cloudRemoteDialogButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.cloudRemoteDialogTitleBar, 1, 0, false).
		AddItem(r.cloudRemoteDialogContent, 0, 1, true)
}

// cloudRemoteDialogFormHeight is tview.Form's own top/bottom border
// padding (2, SetBorderPadding(1,1,1,1) — confirmed the same way
// newConnectLayout's own doc comment already established for a plain
// Form) plus one row per field (backend DropDown, Name, the backend's
// own sub-option dropdown if it has one, each of its plain fields)
// plus one gap row between every pair of them.
func cloudRemoteDialogFormHeight(backend cloudRemoteBackend) int {
	rows := 2 // backend type, name
	if backend.subOptionKey != "" {
		rows++
	}
	rows += len(backend.fields)
	return 2 + rows + (rows - 1)
}

// rebuildCloudRemoteDialogFields rebuilds cloudRemoteDialogForm from
// scratch against cloudRemoteBackends[backendIndex] — tview.Form has
// no clean way to add/remove fields in place, so the whole form is
// cleared and re-added every time the backend selection changes, the
// same shape resetSedForm's own doc comment already establishes for
// an identical "the field set itself depends on a choice made inside
// the form" problem.
func (r *Root) rebuildCloudRemoteDialogFields(backendIndex int) {
	r.cloudRemoteDialogBackendIndex = backendIndex
	backend := cloudRemoteBackends[backendIndex]

	r.cloudRemoteDialogForm.Clear(true)
	// Clear(true) wipes any Form-level color Set*Color call already
	// made (background/label/field colors are themselves just Form
	// state, the same as its items) — re-applied every rebuild, not
	// just once from applyTheme, so a backend-type change never
	// silently reverts to tview's own unthemed default colors.
	r.styleCloudRemoteDialogForm()

	r.cloudRemoteDialogBackendField = tview.NewDropDown().SetLabel("Type")
	labels := make([]string, len(cloudRemoteBackends))
	for i, b := range cloudRemoteBackends {
		labels[i] = b.label
	}
	r.cloudRemoteDialogBackendField.SetOptions(labels, func(text string, index int) {
		if index < 0 || index == r.cloudRemoteDialogBackendIndex {
			return
		}
		r.rebuildCloudRemoteDialogFields(index)
		r.resizeCloudRemoteDialog()
	})
	r.cloudRemoteDialogBackendField.SetCurrentOption(backendIndex)
	r.cloudRemoteDialogForm.AddFormItem(r.cloudRemoteDialogBackendField)

	r.cloudRemoteDialogNameField = tview.NewInputField().SetLabel("Name")
	r.cloudRemoteDialogNameField.SetPlaceholder("a-z/0-9/_/-/./+/@/space, max 32 chars")
	r.cloudRemoteDialogNameField.SetAcceptanceFunc(cloudRemoteNameAcceptanceFunc)
	r.cloudRemoteDialogForm.AddFormItem(r.cloudRemoteDialogNameField)

	if backend.subOptionKey != "" {
		r.cloudRemoteDialogSubField = tview.NewDropDown().SetLabel(backend.subOptionLabel)
		subLabels := make([]string, len(backend.subOptions))
		for i, opt := range backend.subOptions {
			subLabels[i] = opt.display
		}
		r.cloudRemoteDialogSubField.SetOptions(subLabels, nil)
		r.cloudRemoteDialogSubField.SetCurrentOption(0)
		r.cloudRemoteDialogForm.AddFormItem(r.cloudRemoteDialogSubField)
	} else {
		r.cloudRemoteDialogSubField = nil
	}

	r.cloudRemoteDialogFields = make([]*tview.InputField, len(backend.fields))
	for i, spec := range backend.fields {
		field := tview.NewInputField().SetLabel(spec.label)
		placeholder := spec.placeholder
		if spec.required && placeholder == "" {
			placeholder = "required"
		}
		field.SetPlaceholder(placeholder)
		if spec.masked {
			field.SetMaskCharacter('*')
		}
		r.cloudRemoteDialogFields[i] = field
		r.cloudRemoteDialogForm.AddFormItem(field)
	}

	// Last field's own Tab/Enter would otherwise wrap back to the
	// backend DropDown instead of ever reaching the buttons — the same
	// gap newConnectForm's own doc comment already found and fixed for
	// the Connect dialog's own last field. Re-wired here, every
	// rebuild, since which field is actually last changes per backend.
	if n := len(r.cloudRemoteDialogFields); n > 0 {
		last := r.cloudRemoteDialogFields[n-1]
		last.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyTab:
				r.app.SetFocus(r.cloudRemoteDialogCancelBtn)
				return nil
			case tcell.KeyEnter:
				// OK, not Create — the same "OK is the default,
				// Enter in any field runs it" contract the Connect/
				// cloud-mount dialogs' own last field already follow.
				r.runSaveCloudRemoteLenient()
				return nil
			}
			return event
		})
	}

	r.styleCloudRemoteDialogFields()
}

// styleCloudRemoteDialogFields applies the current theme to every
// field individually — see styleCloudMountDialogFields' own doc
// comment for why the Form-level colors styleCloudRemoteDialogForm
// already sets aren't enough on their own (every field with a
// placeholder ignores them; tview.DropDown's own closed-box style
// isn't Form-driven at all). Called at the end of
// rebuildCloudRemoteDialogFields (every backend-type change rebuilds
// every field from scratch) and from applyTheme (a live color-scheme
// switch while already open).
func (r *Root) styleCloudRemoteDialogFields() {
	styleDropDown(r.cloudRemoteDialogBackendField, r.theme)
	if r.cloudRemoteDialogSubField != nil {
		styleDropDown(r.cloudRemoteDialogSubField, r.theme)
	}
	// false, not a per-field focused state — see
	// styleCloudMountDialogFields' own doc comment in
	// cloudmountdialog.go: tview.Form.Draw re-applies its own single
	// fieldStyle to every FormItem on every frame (confirmed directly
	// against form.go/inputfield.go/dropdown.go), so no per-field
	// focused/unfocused value-area color is reachable here at all —
	// true for every Form-based dialog in this app, not specific to
	// this one. false still matters for the parts Form.Draw doesn't
	// touch: placeholder style and the label's own background.
	styleInput(r.cloudRemoteDialogNameField, r.theme, false)
	for _, field := range r.cloudRemoteDialogFields {
		styleInput(field, r.theme, false)
	}
}

// resizeCloudRemoteDialog recomputes the dialog's own rect and the
// form's own height within it — called on open and every time the
// backend selection changes the field count (see
// rebuildCloudRemoteDialogFields's own SelectedFunc), the same
// "content-driven resize" resizeProperties already establishes for a
// window whose size depends on what's actually in it right now.
func (r *Root) resizeCloudRemoteDialog() {
	formHeight := cloudRemoteDialogFormHeight(cloudRemoteBackends[r.cloudRemoteDialogBackendIndex])
	// 90, not the Connect dialog's own 64 — this form's own longest
	// label ("Secret Access Key", 18 chars) is almost twice
	// Connect's own longest ("Identifier", 10), and tview.Form sizes
	// every field's input area to whatever's left after its one
	// shared label column — the same fixed 64 budget that works fine
	// for Connect's short labels left noticeably less room for an
	// actual value here, a real, reported complaint.
	width, height := 90, formHeight+2 // +1 status row, +1 button row
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.cloudRemoteDialogLayout.SetRect(x, y, width, height)
	r.cloudRemoteDialogContent.ResizeItem(r.cloudRemoteDialogForm, formHeight, 0)
}

// openCloudRemoteDialog shows the dialog, fresh — backend reset to
// the first entry (S3) and every field cleared, the same "never
// leaks a previous open's own typed values" contract openConnectDialog
// already keeps for the Connect dialog.
//
// pushOverlay, deliberately not showOverlay: this stacks on top of
// the "New cloud mount" dialog that opened it (see its own Remote
// dropdown handler) rather than replacing it — per a real, reported
// dead end, Escape/Cancel here now pops back to that dialog instead
// of closing everything. runCreateCloudRemote's own success path still
// calls openCloudMountCreateDialog afterwards, which does use
// showOverlay and so still fully refreshes/replaces the stack at that
// point — this only changes what happens on the way in, and on
// Cancel.
func (r *Root) openCloudRemoteDialog() {
	r.rebuildCloudRemoteDialogFields(0)
	r.cloudRemoteDialogNameField.SetText("")
	r.resizeCloudRemoteDialog()
	r.pushOverlay(cloudRemoteDialogPage, r.cloudRemoteDialogLayout, nil)
}

// cloudRemoteParams builds the "key=value" params
// rclone.CreateStaticRemote needs from backend's own field table,
// subOptionIndex (ignored unless backend actually has a sub-option)
// and fieldValues (same length/order as backend.fields) — pure, no
// tview involved, so every backend's own field table can be exercised
// by a test without any widget ever existing. Returns the label of
// each required field left blank instead of building params at all,
// so a caller can refuse before ever reaching CreateStaticRemote (see
// runCreateCloudRemote) — a real `rclone config create` run with a
// required field missing was confirmed to silently create a
// half-configured, unusable remote rather than refusing itself.
func cloudRemoteParams(backend cloudRemoteBackend, subOptionIndex int, fieldValues []string) (params, missing []string) {
	if backend.subOptionKey != "" && subOptionIndex >= 0 && subOptionIndex < len(backend.subOptions) {
		params = append(params, backend.subOptionKey+"="+backend.subOptions[subOptionIndex].value)
	}
	for i, spec := range backend.fields {
		val := ""
		if i < len(fieldValues) {
			val = strings.TrimSpace(fieldValues[i])
		}
		if val == "" {
			if spec.required {
				missing = append(missing, spec.label)
			}
			continue
		}
		params = append(params, spec.key+"="+val)
	}
	return params, missing
}

// runCreateCloudRemote validates the current form and, if valid,
// calls rclone.CreateStaticRemote — on success, hands straight back to
// the "New cloud mount" dialog with the new remote preselected (see
// that dialog's own doc comment on why creating and mounting stay two
// separate steps regardless).
func (r *Root) runCreateCloudRemote() {
	name := strings.TrimSpace(r.cloudRemoteDialogNameField.GetText())
	if !validCloudRemoteName(name) {
		r.showError(fmt.Errorf("%q is not a valid remote name", name))
		return
	}

	backend, subIndex, fieldValues := r.currentCloudRemoteFields()
	params, missing := cloudRemoteParams(backend, subIndex, fieldValues)
	if len(missing) > 0 {
		r.showError(fmt.Errorf("%s: missing required field(s): %s", backend.label, strings.Join(missing, ", ")))
		return
	}

	if err := rclone.CreateStaticRemote(name, backend.backendType, params); err != nil {
		r.showError(err)
		return
	}
	r.hideOverlay()
	r.openCloudMountCreateDialog(name)
}

// currentCloudRemoteFields reads the currently selected backend, its
// own sub-option index (-1 if it has none), and every plain field's
// own current text — shared by runCreateCloudRemote/
// runSaveCloudRemoteLenient/runTestCloudRemote so the three can never
// drift apart in how they read the form.
func (r *Root) currentCloudRemoteFields() (backend cloudRemoteBackend, subIndex int, fieldValues []string) {
	backend = cloudRemoteBackends[r.cloudRemoteDialogBackendIndex]
	subIndex = -1
	if backend.subOptionKey != "" && r.cloudRemoteDialogSubField != nil {
		subIndex, _ = r.cloudRemoteDialogSubField.GetCurrentOption()
	}
	fieldValues = make([]string, len(r.cloudRemoteDialogFields))
	for i, f := range r.cloudRemoteDialogFields {
		fieldValues[i] = f.GetText()
	}
	return backend, subIndex, fieldValues
}

// runSaveCloudRemoteLenient is the "OK" button's own action: persists
// the remote exactly like Create does, except it never refuses for a
// missing required field — whatever's filled in gets saved, via
// CreateStaticRemote, just like Create. This writes a remote to
// rclone's own config that may not actually work yet, same as plain
// `rclone config create` with a required field left out does on its
// own (see cloudRemoteFieldSpec's own doc comment on that confirmed
// behavior) — the difference is this is now an explicit, informed
// choice the user made by pressing a button named "OK" right next to
// "Create", not a silent side effect of a tool that never warned
// about it.
func (r *Root) runSaveCloudRemoteLenient() {
	name := strings.TrimSpace(r.cloudRemoteDialogNameField.GetText())
	if !validCloudRemoteName(name) {
		r.showError(fmt.Errorf("%q is not a valid remote name", name))
		return
	}

	backend, subIndex, fieldValues := r.currentCloudRemoteFields()
	params, _ := cloudRemoteParams(backend, subIndex, fieldValues)

	if err := rclone.CreateStaticRemote(name, backend.backendType, params); err != nil {
		r.showError(err)
		return
	}
	r.hideOverlay()
	r.openCloudMountCreateDialog(name)
}

// cloudRemoteTestTimeout bounds both TestRemote's own connect and I/O
// timeouts — long enough for a real, reachable endpoint to answer,
// short enough that a genuinely unreachable one fails fast rather
// than leaving the dialog's own "Testing…" status sitting there for
// however long rclone's own defaults would otherwise take (confirmed:
// a full minute, via --contimeout's own default, before TestRemote's
// own timeout parameter existed to override it).
const cloudRemoteTestTimeout = 10 * time.Second

// setCloudRemoteDialogStatus mirrors setConnectStatus's own shape
// exactly, for this dialog's own status line.
func (r *Root) setCloudRemoteDialogStatus(text string, color tcell.Color) {
	r.cloudRemoteDialogStatus.SetTextColor(color)
	r.cloudRemoteDialogStatus.SetText(text)
}

// obscureTestParams re-obscures the value of every "key=value" entry
// in params whose key is flagged obscure:true in backend's own field
// table — see ObscurePassword's own doc comment on why TestRemote's
// on-the-fly connection string needs this despite CreateStaticRemote
// (plain `rclone config create`) needing no such step itself.
func obscureTestParams(backend cloudRemoteBackend, params []string) ([]string, error) {
	needsObscure := make(map[string]bool)
	for _, spec := range backend.fields {
		if spec.obscure {
			needsObscure[spec.key] = true
		}
	}
	out := make([]string, len(params))
	for i, p := range params {
		key, value, ok := strings.Cut(p, "=")
		if !ok || !needsObscure[key] {
			out[i] = p
			continue
		}
		obscured, err := rclone.ObscurePassword(value)
		if err != nil {
			return nil, err
		}
		out[i] = key + "=" + obscured
	}
	return out, nil
}

// runTestCloudRemote is the "Test" button's own action: a real
// connectivity check against whatever the form currently holds, via
// rclone.TestRemote's own on-the-fly connection string — nothing ever
// persisted, regardless of whether required fields are filled in (a
// real connection failure naming what's actually wrong is more useful
// feedback than a pre-check here duplicating it). Runs via safeGo:
// TestRemote shells out to a real rclone process and can take up to
// cloudRemoteTestTimeout to fail, which must never block the UI
// thread, the same non-blocking-background-work rule toggleCloudMount
// already applies to its own real Mount/Unmount calls.
func (r *Root) runTestCloudRemote() {
	backend, subIndex, fieldValues := r.currentCloudRemoteFields()
	params, _ := cloudRemoteParams(backend, subIndex, fieldValues)

	r.setCloudRemoteDialogStatus("Testing…", r.theme.Text)
	onPanic := func() { r.setCloudRemoteDialogStatus("", r.theme.Text) }
	r.safeGo("cloud remote test", onPanic, func() {
		testParams, err := obscureTestParams(backend, params)
		if err == nil {
			err = rclone.TestRemote(backend.backendType, testParams, cloudRemoteTestTimeout)
		}
		r.app.QueueUpdateDraw(func() {
			if err != nil {
				r.setCloudRemoteDialogStatus("✕ "+err.Error(), r.theme.CriticalText)
			} else {
				r.setCloudRemoteDialogStatus("✓ Connection OK", r.theme.EntryExecutable)
			}
		})
	})
}
