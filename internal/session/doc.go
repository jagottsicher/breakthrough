// Package session manages breakthrough's per-run session identity and the
// trash path derived from it. A session ID is generated at startup (not
// just the PID, which gets recycled) and is used to build a session-scoped
// trash location such as $XDG_RUNTIME_DIR/breakthrough/trash/<user>/<id>/,
// the default for a trash that does not survive past the session.
package session
