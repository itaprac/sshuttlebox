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
- Preview generated SSH commands with `--dry-run`
- Optional password-based automatic login
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

Connect to it:

```bash
shbx connect prod
```

Preview the command without connecting:

```bash
shbx connect prod --dry-run
```

## Usage

```bash
shbx add [name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]
shbx list
shbx show <name>
shbx connect <name> [--dry-run|--print]
shbx edit <name> [--name new-name] [--host host] [--user user] [--password password] [--port port] [--identity-file path]
shbx remove <name> [--yes|--force]
```

Run `shbx help` for the full command reference.

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
