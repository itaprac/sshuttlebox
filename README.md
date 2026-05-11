<div align="center">

# sshuttlebox

**A compact SSH host and tunnel manager for your terminal.**

Save SSH targets under short names, organize them into groups, manage local/remote/SOCKS tunnels, and connect with a single command — or browse everything from an interactive TUI.

[![CI](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml/badge.svg)](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml)
[![Website](https://img.shields.io/badge/website-live-2ea043)](https://itaprac.github.io/sshuttlebox/)
[![Go Version](https://img.shields.io/github/go-mod/go-version/itaprac/sshuttlebox)](https://github.com/itaprac/sshuttlebox/blob/main/go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/itaprac/sshuttlebox.svg)](https://pkg.go.dev/github.com/itaprac/sshuttlebox)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)](#installation)

</div>

---

## Table of Contents

- [Why sshuttlebox?](#why-sshuttlebox)
- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage](#usage)
  - [Hosts](#hosts)
  - [Tunnels](#tunnels)
  - [Groups](#groups)
- [Command Reference](#command-reference)
- [Shell Completion](#shell-completion)
- [Configuration](#configuration)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Why sshuttlebox?

SSH workflows tend to grow into long commands, copied notes, and repeated tunnel
setup. `sshuttlebox` keeps hosts, groups, and tunnels in a single local config
file so you can reach common targets quickly — from the command line or from an
interactive terminal UI.

## Features

- **Saved hosts** — connect to any SSH target by a short name
- **Interactive TUI** — browse, edit, and connect without remembering flags
- **Command palette** — run common TUI actions with `:` or `ctrl+p`
- **Tunnels** — local, remote, and SOCKS forwarding with start/stop lifecycle
- **Groups** — organize related hosts and tunnels for quick filtering
- **Shell completion** — bash, zsh, and fish for all saved names
- **Doctor checks** — inspect config, SSH, keys, and tunnel state
- **Dry-run preview** — see the underlying `ssh` command before connecting
- **Password auth (optional)** — keys are preferred, but passwords are supported

## Installation

> Requires **Go 1.22 or newer**. Supported on **macOS** and **Linux**.

### Using `go install`

```bash
go install github.com/itaprac/sshuttlebox/cmd/shbx@latest
```

Make sure your Go binary directory is in `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

### From source

```bash
git clone https://github.com/itaprac/sshuttlebox.git
cd sshuttlebox
go build -o shbx ./cmd/shbx
```

## Quick Start

Initialize the local config, add a host, and connect:

```bash
shbx config init
shbx add prod --host 192.0.2.10 --user deploy --identity-file ~/.ssh/id_ed25519
shbx connect prod
```

Open the interactive terminal UI:

```bash
shbx
```

Preview the underlying SSH command without connecting:

```bash
shbx connect prod --dry-run
```

Check local setup health:

```bash
shbx doctor
```

## Usage

### Hosts

```bash
shbx add staging --host 198.51.100.5 --user deploy --port 2222
shbx list
shbx show staging
shbx edit staging
shbx remove staging
```

### Tunnels

Add and start a **local** port-forwarding tunnel through a saved host:

```bash
shbx tunnel add db --host prod --local-port 5432 --remote-host 127.0.0.1 --remote-port 5432
shbx tunnel start db
shbx tunnel stop db
```

Add and start a **SOCKS** (dynamic) tunnel:

```bash
shbx tunnel add socks --host prod --dynamic-port 1080
shbx tunnel start socks
```

### Groups

Group related hosts and tunnels for easier navigation:

```bash
shbx group add work
shbx add prod --host 192.0.2.10 --group work
shbx group show work
```

## Command Reference

| Command | Description |
| --- | --- |
| `shbx` / `shbx ui` | Open the interactive terminal UI |
| `shbx add [name]` | Add or update a saved SSH host |
| `shbx list` | List saved hosts |
| `shbx show <name>` | Show saved host details |
| `shbx connect <name>` | Connect to a saved host |
| `shbx edit <name>` | Edit a saved host |
| `shbx remove <name>` | Remove a saved host |
| `shbx tunnel ...` | Add, list, show, start, stop, or remove SSH tunnels |
| `shbx group ...` | Add, list, show, rename, or remove groups |
| `shbx completion ...` | Generate or install shell completion |
| `shbx doctor` | Check config, SSH, keys, and tunnels |
| `shbx config ...` | Initialize or inspect the local config |
| `shbx version` | Print the installed version |
| `shbx help` | Show the full command reference |

For full options and examples, run:

```bash
shbx help
```

## Shell Completion

Install completion for your current shell:

```bash
shbx completion install
```

Install completion for bash, zsh, and fish at once:

```bash
shbx completion install --shell all
```

Completion files are written to user-level paths:

| Shell | Path |
| --- | --- |
| bash | `~/.local/share/shbx/completions/bash/shbx` |
| zsh  | `~/.local/share/shbx/completions/zsh/_shbx` |
| fish | `~/.config/fish/completions/shbx.fish` |

You can also print a completion script to stdout:

```bash
shbx completion bash
shbx completion zsh
shbx completion fish
```

## Configuration

Hosts, tunnels, and groups are stored in a single JSON file:

```text
~/.config/sshuttlebox/config.json
```

Useful config commands:

```bash
shbx config path     # print the config file path
shbx config status   # show config health and counts
```

> **Note on credentials:** saved passwords are stored in the local config file.
> Prefer SSH keys whenever possible.

## Development

```bash
# Run the full test suite
go test ./...

# Run the CLI directly from source
go run ./cmd/shbx help

# Build a local binary
go build -o shbx ./cmd/shbx
```

## Contributing

Contributions are welcome. Please open an issue to discuss substantial changes
before submitting a pull request.

## License

Released under the [MIT License](LICENSE).
