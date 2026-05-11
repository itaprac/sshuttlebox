# sshuttlebox

[![CI](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml/badge.svg)](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml)

`sshuttlebox` is a compact SSH host and tunnel manager for the terminal. It
stores named SSH targets locally and lets you connect through the short `shbx`
command.

```bash
shbx add prod --host 192.0.2.10 --user deploy
shbx connect prod
```

## Why

SSH workflows often grow into long commands, copied notes, and repeated tunnel
setup. `sshuttlebox` keeps hosts, groups, and tunnels in a local config file so
you can reach common targets quickly from the command line or terminal UI.

## Features

- Saved SSH hosts under short names
- Interactive terminal UI
- Local, remote, and SOCKS tunnels
- Groups for hosts and tunnels
- Shell completion for saved names
- Dry-run command preview
- Optional password-based login

## Installation

Requires Go 1.22 or newer.

```bash
go install github.com/itaprac/sshuttlebox/cmd/shbx@latest
```

Make sure your Go binary directory is in `PATH`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

Supported platforms: macOS and Linux.

## Quick Start

Create the local config file, add a host, and connect:

```bash
shbx config init
shbx add prod --host 192.0.2.10 --user deploy --identity-file ~/.ssh/id_ed25519
shbx connect prod
```

Open the terminal UI:

```bash
shbx
```

Preview the SSH command without connecting:

```bash
shbx connect prod --dry-run
```

## Tunnels

Add and start a local port-forwarding tunnel through a saved host:

```bash
shbx tunnel add db --host prod --local-port 5432 --remote-host 127.0.0.1 --remote-port 5432
shbx tunnel start db
shbx tunnel stop db
```

Add and start a SOCKS tunnel:

```bash
shbx tunnel add socks --host prod --dynamic-port 1080
shbx tunnel start socks
```

## Groups

Group related hosts and tunnels:

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
| `shbx config ...` | Initialize or inspect the local config |
| `shbx version` | Print the installed version |
| `shbx help` | Show the full command reference |

Run the built-in help for all options and examples:

```bash
shbx help
```

## Shell Completion

Install completion for your current shell:

```bash
shbx completion install
```

Install completion files for bash, zsh, and fish:

```bash
shbx completion install --shell all
```

Completion files are installed in user-level paths:

```text
bash: ~/.local/share/shbx/completions/bash/shbx
zsh:  ~/.local/share/shbx/completions/zsh/_shbx
fish: ~/.config/fish/completions/shbx.fish
```

You can also print a completion script:

```bash
shbx completion bash
shbx completion zsh
shbx completion fish
```

## Configuration

Hosts, tunnels, and groups are stored in:

```text
~/.config/sshuttlebox/config.json
```

Useful config commands:

```bash
shbx config path
shbx config status
```

Saved passwords are stored in the local config file. Prefer SSH keys when
possible.

## Development

```bash
go test ./...
go run ./cmd/shbx help
go build -o shbx ./cmd/shbx
```

## License

MIT
