# sshuttlebox / shbx

[![CI](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml/badge.svg)](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml)

`sshuttlebox` is a small CLI for saving SSH hosts and connecting to them with the `shbx` command.

It stores hosts locally in `~/.config/sshuttlebox/config.json` and uses the system `ssh` command for connections.
Saved passwords can be used for automatic login.

## Features

- Add SSH hosts interactively or with flags
- List saved hosts with `NAME`, `TARGET`, and `KEY`
- Preview SSH commands with `connect --dry-run`
- Save SSH passwords and connect without external password helpers
- Connect, edit, rename, and remove saved hosts
- Shell autocomplete for saved host names
- Confirmation prompts for overwrites and removals
- Warnings for missing SSH identity files

## Install

Requires Go 1.22+.

```bash
go install github.com/itaprac/sshuttlebox/cmd/shbx@latest
```

Update to the latest version with the same command:

```bash
go install github.com/itaprac/sshuttlebox/cmd/shbx@latest
```

Supported targets: macOS and Linux.

## Usage

```bash
shbx config init
shbx add
shbx add prod --host 192.0.2.10 --user deploy --port 22 --identity-file ~/.ssh/id_ed25519
shbx add legacy --host 192.0.2.20 --user admin --password 'secret'
shbx list
shbx show prod
shbx connect prod --dry-run
shbx connect prod
shbx edit prod --name staging --host 192.0.2.11 --user deploy --password '' --port 2222
shbx remove staging
shbx remove staging --yes
```

## Shell autocomplete

`shbx` can generate completion scripts that autocomplete saved host names for
`connect`, `show`, `edit`, and `remove`.

Install completion for your current shell:

```bash
shbx completion install
```

Or install completion files for every supported shell:

```bash
shbx completion install --shell all
```

The installer uses user-level paths, so it works without `sudo` on macOS and
Linux:

```text
bash: ~/.local/share/shbx/completions/bash/shbx
zsh:  ~/.local/share/shbx/completions/zsh/_shbx
fish: ~/.config/fish/completions/shbx.fish
```

For bash and zsh, `shbx completion install` also adds a small startup block to
`~/.bashrc`, `~/.bash_profile` on macOS, or `~/.zshrc`. Fish loads completions
from `~/.config/fish/completions` automatically.

If you only want to print a script:

```bash
shbx completion bash
shbx completion zsh
shbx completion fish
```

For package managers, install the printed scripts into the package manager's
completion directory instead, for example:

```bash
shbx completion zsh > /usr/local/share/zsh/site-functions/_shbx
```

Example list output:

```text
NAME             TARGET                       KEY
prod             deploy@192.0.2.10:22         ~/.ssh/id_ed25519
```

## Development

```bash
go test ./...
go run ./cmd/shbx help
go build -o shbx ./cmd/shbx
```
