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
- `shbx add <name>`
- `shbx list`
- `shbx show <name>`
- `shbx connect <name>`
- `shbx remove <name>`

Konfiguracja będzie trzymana w:

```text
~/.config/sshuttlebox/config.json
```

na macOS zwykle:

```text
~/Library/Application Support/sshuttlebox/config.json
```

## Docelowy MVP

```bash
shbx add <name>
shbx list
shbx show <name>
shbx connect <name>
shbx remove <name>
```

## Development

Wymagany Go 1.22+.

```bash
go run ./cmd/shbx help
go run ./cmd/shbx config init
go run ./cmd/shbx config status
go run ./cmd/shbx add prod --host 192.0.2.10 --user deploy --port 22 --identity-file ~/.ssh/id_ed25519
go run ./cmd/shbx list
go run ./cmd/shbx show prod
go run ./cmd/shbx connect prod
go run ./cmd/shbx remove prod
```
