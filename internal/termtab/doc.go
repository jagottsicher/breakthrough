// Package termtab is feature_ideas.txt #11's own first phase ("Eingebetteter
// Remote-Terminal-Tab"): a live, PTY-backed SSH session with a real virtual
// terminal emulator (github.com/hinshun/vt10x) parsing its output into a
// cell buffer — the piece internal/ui's own Draw(screen tcell.Screen) can
// read directly, the same custom-tview.Primitive shape scrollbar.go and
// easteregg.go already establish, once a later phase builds that widget.
//
// Deliberately UI-free, no tview/tcell import here at all — only Session's
// own thin API surface (Cell/Cursor/Size/Resize/WriteInput) crosses that
// boundary, the same split internal/mail/internal/filemeta already keep
// from internal/ui.
//
// Auth/connection reuse: Session.Start's own real command construction is
// exec.Command("ssh", sshshell.Argv(conn)...) — the identical argv builder
// the existing Suspend+exec SSH shell launcher (internal/ui/sshshell.go,
// chord "gs") already uses, per the user's own explicit decision that this
// feature needs no second, parallel auth mechanism.
package termtab
