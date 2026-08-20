[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-brightgreen.svg)](https://opensource.org/licenses/Apache-2.0)
[![twitter](https://badges.aleen42.com/src/twitter.svg)](https://twitter.com/jagottsicher)
[![twitter](https://badges.aleen42.com/src/wechat.svg)](weixin://dl/chat?jagottsicher)

# breakthrough
## A window/menu-based TUI file manager for your POSIX-compliant terminal, written in Go.

breakthrough puts a quasi-windows-like layer — panels, floating overlays,
context menus, a taskbar — over your terminal session, without leaving the
shell. It's built with [tcell](https://github.com/gdamore/tcell) and
[tview](https://github.com/rivo/tview); it is not integrated into Bash
itself. The shell connection stays a thin wrapper (aliases, shell
functions, readline keybindings), never code integration into Bash.

It is explicitly not an attempt to rebuild Midnight Commander — the goal is
its own UX philosophy, closer to classic GUI file managers, just in the
terminal.

**Status:** early rewrite. The original C/ncurses proof of concept has been
retired in favor of a clean Go implementation; there is no working build
yet. See the [whitepaper](docs/whitepaper.md) for the full concept and
vision, and follow along or join in on
[Discussions](https://github.com/jagottsicher/breakthrough/discussions).

## Contributing

Contributions are very welcome — this is an ambitious project for one
person alone. See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, the
branch workflow, and code style expectations.

## Support

The best way to support breakthrough is to participate and contribute. If
you'd like to leave a tip instead, you can send a few Satoshis via the
Lightning Network ⚡️ using the sponsor button above.
