// Package mail is Stufe 1 of feature_ideas.txt's own "0f.
// E-Mail-Client-Integration": detecting an already-installed terminal
// mail client and building the command line to launch it — never a
// mail client of its own, no IMAP/SMTP/Maildir/mbox reading here (see
// feature_ideas.txt's own later, explicitly separate "Ungelesen-
// Zähler" stage for that). The same "shell out to the real program,
// real terminal, no reimplementation" approach internal/multiplex
// already takes for screen/tmux/Zellij and internal/sshkeys takes for
// ssh-keygen/ssh-copy-id.
package mail
