package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/sshkeys"
)

// The SSH Keys screen's own "Generate key" form ("a", see sshkeys.go's
// own captureSSHKeysKey) — Stage 2 of feature_ideas.txt's own
// SSH-Key-Verwaltung entry. Building a key pair never hand-assembles
// `ssh-keygen` syntax itself: it fills in sshkeys.GenerateSpec, asks
// sshkeys.GenerateCommand for the exact command that would create it,
// and shows that literal command in a confirmation dialog before ever
// running it — the same "irreversible action sichtbar machen und
// bestätigen" discipline firewalladdrule.go's own "Add rule" form
// already follows for the identical reason.
//
// Always generates into ~/.ssh under a filename the user picks, never a
// free-choice path, and refuses outright if that name is already taken
// (see sshkeys.FilenameAvailable) — generating a new key pair can never
// silently overwrite an existing one.
//
// Deliberately has no passphrase field at all: the confirmed command
// never carries `-N`, so `ssh-keygen` itself prompts for one
// interactively (through the real, attached terminal this runs in, see
// runSSHKeysGenerateCommandFullScreen) — the only way to set one that
// never touches this process's own argv or a shell's history, the same
// reasoning sshkeys.GenerateCommand's own doc comment gives.
const sshKeysGeneratePage = "sshkeys-generate"

// sshKeysGenerateAlgorithmChoices is the "Generate key" form's own
// Algorithm dropdown, in display order — dsa is deliberately not
// offered (see sshkeys.GenerateSpec's own doc comment).
var sshKeysGenerateAlgorithmChoices = []string{"ed25519", "rsa", "ecdsa"}

// openSSHKeysGenerate resets the form to its own defaults and shows it
// layered on top of the still-open SSH Keys screen (pushOverlay, the
// same layering firewalladdrule.go's own openFirewallAddRule uses over
// the Firewall screen).
func (r *Root) openSSHKeysGenerate() {
	r.sshKeysGenerateAlgorithm = "ed25519"
	r.sshKeysGenerateBits = RSABitsChoices[len(RSABitsChoices)-1]
	r.sshKeysGenerateFilenameText = defaultSSHKeysGenerateFilename(r.sshKeysGenerateAlgorithm)
	r.sshKeysGenerateCommentText = defaultSSHKeysComment()
	r.setSSHKeysGenerateStatus("", r.theme.Text)
	r.renderSSHKeysGenerateForm()

	width, height := 62, 11
	_, _, screenWidth, screenHeight := r.GetRect()
	if width > screenWidth-4 {
		width = screenWidth - 4
	}
	if height > screenHeight-4 {
		height = screenHeight - 4
	}
	x := (screenWidth - width) / 2
	y := (screenHeight - height) / 2
	r.sshKeysGenerateLayout.SetRect(x, y, width, height)
	r.pushOverlay(sshKeysGeneratePage, r.sshKeysGenerateLayout, nil)
}

// defaultSSHKeysComment mirrors `ssh-keygen`'s own default comment
// (user@host) — a real, likely-useful starting point rather than
// leaving the field blank for absolutely everyone to fill in by hand.
// Falls back to whichever half is actually available (or "" for
// neither) rather than failing this whole form's own open over a
// cosmetic default.
func defaultSSHKeysComment() string {
	user := os.Getenv("USER")
	host, _ := os.Hostname()
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case user != "":
		return user
	default:
		return host
	}
}

// defaultSSHKeysGenerateFilename is this form's own suggested filename
// for a freshly chosen algorithm — "id_<algorithm>", the same naming
// `ssh-keygen` itself defaults to. Used both to seed the form on open
// and to tell an untouched default apart from a filename the user
// already typed themselves (see renderSSHKeysGenerateForm's own
// Algorithm callback).
func defaultSSHKeysGenerateFilename(algorithm string) string {
	return "id_" + algorithm
}

// RSABitsChoices/ECDSABitsChoices re-exported at package level for this
// form's own dropdown — see sshkeys.RSABitsChoices/ECDSABitsChoices for
// why these exact, short lists and no others.
var (
	RSABitsChoices   = sshkeys.RSABitsChoices
	ECDSABitsChoices = sshkeys.ECDSABitsChoices
)

// newSSHKeysGenerateForm builds the (initially empty) "Generate key"
// form — called once from NewRoot; renderSSHKeysGenerateForm populates
// it fresh on every open and on every Algorithm change, the same
// reasoning newFirewallAddRuleForm's own doc comment gives.
func (r *Root) newSSHKeysGenerateForm() *tview.Form {
	f := tview.NewForm()
	f.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			r.cancelSSHKeysGenerate()
			return nil
		}
		return event
	})
	return f
}

// renderSSHKeysGenerateForm (re)builds sshKeysGenerateForm's own items:
// Algorithm (a dropdown that rebuilds this whole form on change, the
// same guarded-against-the-synthetic-initial-call shape
// renderDuplicateForm's own Strategy dropdown already establishes, since
// only rsa/ecdsa carry a Bits field at all), Bits/curve (rsa/ecdsa only),
// Filename, and Comment.
func (r *Root) renderSSHKeysGenerateForm() {
	r.sshKeysGenerateForm.Clear(true)

	algoCurrent := 0
	for i, a := range sshKeysGenerateAlgorithmChoices {
		if a == r.sshKeysGenerateAlgorithm {
			algoCurrent = i
		}
	}
	algoField := tview.NewDropDown().SetLabel("Algorithm").SetOptions(sshKeysGenerateAlgorithmChoices, func(_ string, index int) {
		if index < 0 || index >= len(sshKeysGenerateAlgorithmChoices) {
			return
		}
		algorithm := sshKeysGenerateAlgorithmChoices[index]
		if algorithm == r.sshKeysGenerateAlgorithm {
			return
		}
		// Only follows the algorithm change if the filename still is
		// this form's own untouched default for the algorithm it's
		// leaving — a filename the user already typed themselves is
		// never silently overwritten out from under them.
		if r.sshKeysGenerateFilenameText == defaultSSHKeysGenerateFilename(r.sshKeysGenerateAlgorithm) {
			r.sshKeysGenerateFilenameText = defaultSSHKeysGenerateFilename(algorithm)
		}
		r.sshKeysGenerateAlgorithm = algorithm
		switch algorithm {
		case "rsa":
			r.sshKeysGenerateBits = RSABitsChoices[len(RSABitsChoices)-1]
		case "ecdsa":
			r.sshKeysGenerateBits = ECDSABitsChoices[0]
		}
		r.renderSSHKeysGenerateForm()
	})
	algoField.SetCurrentOption(algoCurrent)
	styleDropDown(algoField, r.theme)
	r.sshKeysGenerateForm.AddFormItem(algoField)

	if r.sshKeysGenerateAlgorithm == "rsa" || r.sshKeysGenerateAlgorithm == "ecdsa" {
		choices := RSABitsChoices
		label := "Key size"
		if r.sshKeysGenerateAlgorithm == "ecdsa" {
			choices = ECDSABitsChoices
			label = "Curve size"
		}
		labels := make([]string, len(choices))
		current := 0
		for i, c := range choices {
			labels[i] = strconv.Itoa(c)
			if c == r.sshKeysGenerateBits {
				current = i
			}
		}
		bitsField := tview.NewDropDown().SetLabel(label).SetOptions(labels, func(_ string, index int) {
			if index < 0 || index >= len(choices) {
				return
			}
			r.sshKeysGenerateBits = choices[index]
		})
		bitsField.SetCurrentOption(current)
		styleDropDown(bitsField, r.theme)
		r.sshKeysGenerateForm.AddFormItem(bitsField)
	}

	filenameField := tview.NewInputField().SetLabel("Filename (in ~/.ssh)").SetText(r.sshKeysGenerateFilenameText)
	filenameField.SetChangedFunc(func(v string) { r.sshKeysGenerateFilenameText = v })
	r.sshKeysGenerateForm.AddFormItem(filenameField)

	commentField := tview.NewInputField().SetLabel("Comment (optional)").SetText(r.sshKeysGenerateCommentText)
	commentField.SetChangedFunc(func(v string) { r.sshKeysGenerateCommentText = v })
	commentField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysGenerateCancelBtn)
			return nil
		case tcell.KeyEnter:
			r.submitSSHKeysGenerate()
			return nil
		}
		return event
	})
	r.sshKeysGenerateForm.AddFormItem(commentField)
}

// newSSHKeysGenerateButtons builds sshKeysGenerateForm's own action row
// once, from NewRoot — the same Cancel/action button pair
// newFirewallAddRuleButtons already establishes, Tab/Backtab cycling
// included.
func (r *Root) newSSHKeysGenerateButtons() *tview.Flex {
	r.sshKeysGenerateCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.cancelSSHKeysGenerate)
	r.sshKeysGenerateGenerateBtn = tview.NewButton("Generate key").SetSelectedFunc(r.submitSSHKeysGenerate)
	r.sshKeysGenerateCancelBtn.SetInputCapture(spaceAlsoActivates(r.cancelSSHKeysGenerate))
	r.sshKeysGenerateGenerateBtn.SetInputCapture(spaceAlsoActivates(r.submitSSHKeysGenerate))

	r.sshKeysGenerateCancelBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysGenerateGenerateBtn)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshKeysGenerateForm)
		case tcell.KeyEscape:
			r.cancelSSHKeysGenerate()
		}
	})
	r.sshKeysGenerateGenerateBtn.SetExitFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyTab:
			r.app.SetFocus(r.sshKeysGenerateForm)
		case tcell.KeyBacktab:
			r.app.SetFocus(r.sshKeysGenerateCancelBtn)
		case tcell.KeyEscape:
			r.cancelSSHKeysGenerate()
		}
	})

	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.sshKeysGenerateCancelBtn, 0, 1, false).
		AddItem(r.sshKeysGenerateGenerateBtn, 0, 1, false)
}

// newSSHKeysGenerateLayout wraps sshKeysGenerateTitleBar over the form,
// a one-line status area, and the Cancel/Generate key buttons — the
// same shape newFirewallAddRuleLayout already establishes.
func (r *Root) newSSHKeysGenerateLayout() *tview.Flex {
	r.sshKeysGenerateTitleBar = newPlainTitleBar("Generate key")
	r.sshKeysGenerateStatus = tview.NewTextView().SetDynamicColors(true)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshKeysGenerateForm, 8, 0, true).
		AddItem(r.sshKeysGenerateStatus, 1, 0, false).
		AddItem(r.sshKeysGenerateButtons, 1, 0, false)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.sshKeysGenerateTitleBar, 1, 0, false).
		AddItem(content, 0, 1, true)
}

func (r *Root) setSSHKeysGenerateStatus(text string, color tcell.Color) {
	r.sshKeysGenerateStatus.SetTextColor(color)
	r.sshKeysGenerateStatus.SetText(text)
}

func (r *Root) cancelSSHKeysGenerate() {
	r.hideOverlay()
}

// buildSSHKeysGenerateSpec reads the form's own current value mirrors
// into an sshkeys.GenerateSpec.
func (r *Root) buildSSHKeysGenerateSpec() sshkeys.GenerateSpec {
	return sshkeys.GenerateSpec{
		Algorithm: r.sshKeysGenerateAlgorithm,
		Bits:      r.sshKeysGenerateBits,
		Filename:  r.sshKeysGenerateFilenameText,
		Comment:   r.sshKeysGenerateCommentText,
	}
}

// submitSSHKeysGenerate is the form's own "Generate key" action (button,
// or Enter on the last field): validates the form into a GenerateSpec,
// checks the chosen filename isn't already taken, builds the exact
// `ssh-keygen` command that would create it, and asks for explicit
// confirmation showing that literal command — never running anything
// before the user has seen and accepted exactly what will run. Any
// failure is reported in place, in sshKeysGenerateStatus, without
// closing the form, so it can be fixed and resubmitted immediately.
func (r *Root) submitSSHKeysGenerate() {
	spec := r.buildSSHKeysGenerateSpec()
	if err := spec.Validate(); err != nil {
		r.setSSHKeysGenerateStatus(err.Error(), r.theme.CriticalText)
		return
	}

	dir := sshkeys.DefaultDir()
	if dir == "" {
		r.setSSHKeysGenerateStatus("could not determine the current user's home directory", r.theme.CriticalText)
		return
	}
	if err := sshkeys.FilenameAvailable(dir, spec.Filename); err != nil {
		r.setSSHKeysGenerateStatus(err.Error(), r.theme.CriticalText)
		return
	}

	command, err := sshkeys.GenerateCommand(dir, spec)
	if err != nil {
		r.setSSHKeysGenerateStatus(err.Error(), r.theme.CriticalText)
		return
	}

	message := fmt.Sprintf("Run this exact command now? %s (ssh-keygen will prompt for an optional passphrase next)", command)
	r.openConfirm(message, "Yes, generate key", func() {
		r.applySSHKeysGenerate(command)
	})
}

// applySSHKeysGenerate is submitSSHKeysGenerate's own confirmed action:
// closes the "Generate key" form (openConfirm's own acceptConfirm
// already closed the confirmation dialog itself before calling this —
// see applyFirewallAddRule's own identical comment), then runs the
// command full-screen, via a real terminal, and re-scans ~/.ssh either
// way so the SSH Keys screen underneath reflects the new key pair (or
// a failed attempt) immediately, not a stale pre-attempt read.
func (r *Root) applySSHKeysGenerate(command string) {
	r.hideOverlay() // the "Generate key" form itself — the SSH Keys screen underneath stays open

	runErr := r.runSSHKeysGenerateCommandFullScreen(command)
	r.reloadSSHKeys()

	if runErr != nil {
		r.activityLog.Error(activitylog.CategorySSHKeys, fmt.Sprintf("generate key %q: %v", command, runErr))
		r.showError(fmt.Errorf("ssh-keygen: %w", runErr))
		return
	}
	r.activityLog.Action(activitylog.CategorySSHKeys, fmt.Sprintf("generated key: %s", command))
}

// runSSHKeysGenerateCommandFullScreen suspends the TUI and runs command
// through the real terminal — the same Suspend/echo/wait-for-Escape
// shape runFirewallCommandFullScreen already establishes, minus the
// "sudo " prefix: generating a key pair under ~/.ssh never needs
// elevated privileges. A real terminal is still required regardless,
// since ssh-keygen's own interactive passphrase prompt needs one (see
// this file's own package doc comment).
func (r *Root) runSSHKeysGenerateCommandFullScreen(command string) error {
	var runErr error
	r.app.Suspend(func() {
		fmt.Printf("$ %s\n", command)
		cmd := exec.Command(userShell(), fullScreenShellArgs(command)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
		fmt.Print("\n[Press Esc to return to breakthrough]\n")
		waitForEscape()
	})
	return runErr
}

// applySSHKeysGenerateTheme themes the "Generate key" form — split out
// of applySSHKeysTheme so that function's own guard (nil sshKeysTable)
// doesn't gate this dialog's theming too, the same reasoning
// applyFirewallTheme's own trailing, unconditional block already gives
// for its own three secondary dialogs.
func (r *Root) applySSHKeysGenerateTheme(theme config.ResolvedTheme) {
	r.sshKeysGenerateForm.SetBackgroundColor(theme.SurfaceBackground)
	r.sshKeysGenerateForm.SetLabelColor(theme.TextColor)
	r.sshKeysGenerateForm.SetFieldBackgroundColor(theme.InputFocusedBackground)
	r.sshKeysGenerateForm.SetFieldTextColor(theme.TextColor)
	r.sshKeysGenerateStatus.SetBackgroundColor(theme.SurfaceBackground)
	r.sshKeysGenerateButtons.SetBackgroundColor(theme.SurfaceBackground)
	styleButton(r.sshKeysGenerateCancelBtn, theme)
	styleButton(r.sshKeysGenerateGenerateBtn, theme)
	r.sshKeysGenerateTitleBar.SetBackgroundColor(theme.InputFocusedBackground)
	r.sshKeysGenerateTitleBar.SetTextColor(theme.TextColor)
}
