# sshuttlebox / shbx

[![CI](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml/badge.svg)](https://github.com/itaprac/sshuttlebox/actions/workflows/ci.yml)

`sshuttlebox` is a small CLI for saving SSH hosts and connecting to them with the `shbx` command.

It stores hosts locally in `~/.config/sshuttlebox/config.json` and uses the system `ssh` command for connections.

## Features

- Add SSH hosts interactively or with flags
- List saved hosts with `NAME`, `TARGET`, and `KEY`
- Preview SSH commands with `connect --dry-run`
- Connect, edit, rename, and remove saved hosts
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
shbx list
shbx show prod
shbx connect prod --dry-run
shbx connect prod
shbx edit prod --name staging --host 192.0.2.11 --user deploy --port 2222
shbx remove staging
shbx remove staging --yes
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
