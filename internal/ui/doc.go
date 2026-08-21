// Package ui holds the tview widgets, panels, menus, and dialogs that make
// up breakthrough's terminal interface. Filesystem operations live in
// internal/fsops and must not be called directly from widget callbacks;
// callbacks translate user actions into calls against that package instead.
package ui
