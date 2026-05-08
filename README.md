# sshuttlebox

[![CI](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml/badge.svg)](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml)

`sshuttlebox` is a small SSH connection manager for the terminal. It stores named
SSH hosts locally and lets you connect with the short `shbx` command.

```bash
shbx connect prod
```

## Features

- Save SSH hosts under short names
- Add hosts interactively or with flags
- List, show, edit, rename, and remove saved hosts
- Connect through the system `ssh` command
- Optional terminal UI with search and full host management
- Preview generated SSH commands with `--dry-run`
- Optional password-based automatic login
- Reuse a saved private key path across hosts
- Shell completion for saved host names
- Warnings for missing SSH identity files

## Installation

Requires Go 1.22 or newer.

```bash
go install github.com/itaprac/sshuttlebox/cmd/shbx@latest
```

Make sure your Go binary directory is in `PATH`. For most Go installations:

```bash
export PATH="$HOME/go/bin:$PATH"
```

Supported platforms: macOS and Linux.

## Quick Start

Create the local config file:

```bash
shbx config init
```

Add a host:

```bash
shbx add prod --host 192.0.2.10 --user deploy --port 22 --identity-file ~/.ssh/id_ed25519
```

Reuse a private key already saved on another host:

```bash
shbx add staging --host 192.0.2.11 --user deploy --identity-from prod
```

Connect to it:

```bash
shbx connect prod
```

Preview the command without connecting:

```bash
shbx connect prod --dry-run
```

Open the interactive terminal UI:

```bash
shbx ui
```

## Usage

```bash
shbx add [name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host]
shbx list
shbx show <name>
shbx connect <name> [--dry-run|--print]
shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path|--identity-from host]
shbx remove <name> [--yes|--force]
shbx ui
```

Run `shbx help` for the full command reference.

## Terminal UI

`shbx ui` opens an optional keyboard-first TUI for managing saved hosts. It
supports filtering, adding, editing, removing, dry-run command preview, and
connecting to the selected host. When you connect, the UI exits first and then
starts the normal system `ssh` session. In the add/edit form, press `ctrl+k` by
the identity-file field to choose a private key path already saved on another
host.

## Shell Completion

Install completion for your current shell:

```bash
shbx completion install
```

Install completion files for all supported shells:

```bash
shbx completion install --shell all
```

The installer uses user-level paths and works without `sudo`:

```text
bash: ~/.local/share/shbx/completions/bash/shbx
zsh:  ~/.local/share/shbx/completions/zsh/_shbx
fish: ~/.config/fish/completions/shbx.fish
```

For bash and zsh, `shbx completion install` also adds a small startup block to
`~/.bashrc`, `~/.bash_profile` on macOS, or `~/.zshrc`. Fish loads completions
from `~/.config/fish/completions` automatically.

To print a completion script instead of installing it:

```bash
shbx completion bash
shbx completion zsh
shbx completion fish
```

## Configuration

Hosts are stored in:

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
