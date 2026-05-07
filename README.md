# sshuttlebox / shbx

Terminalowy helper do zapisywania maszyn SSH, szybkiego łączenia i docelowo tuneli oraz kluczy SSH.

## Status

Projekt jest na bardzo wczesnym etapie. Aktualnie mamy fundament CLI i konfiguracji lokalnej.

## Pierwszy zakres

- `shbx help`
- `shbx version`
- `shbx config init`
- `shbx config path`
- `shbx config status`
- `shbx add [name]`
- `shbx list`
- `shbx show <name>`
- `shbx connect <name>`
- `shbx edit <name>`
- `shbx remove <name>`

Konfiguracja jest trzymana w:

```text
~/.config/sshuttlebox/config.json
```

## Docelowy MVP

```bash
shbx add [name]
shbx list
shbx show <name>
shbx connect <name>
shbx connect <name> --dry-run
shbx edit <name>
shbx remove <name>
shbx remove <name> --yes
```

## Development

Wymagany Go 1.22+.

```bash
go run ./cmd/shbx help
go run ./cmd/shbx config init
go run ./cmd/shbx config status
go run ./cmd/shbx add
go run ./cmd/shbx add prod --host 192.0.2.10 --user deploy --port 22 --identity-file ~/.ssh/id_ed25519
go run ./cmd/shbx list
go run ./cmd/shbx show prod
go run ./cmd/shbx connect prod --dry-run
go run ./cmd/shbx connect prod
go run ./cmd/shbx edit prod --name staging --host 192.0.2.11 --user deploy --port 2222
go run ./cmd/shbx remove staging --yes
```
