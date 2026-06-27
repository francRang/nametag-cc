# nametag

A self-updating CLI. On startup it checks GitHub Releases for a newer version,
downloads it, verifies its SHA-256 checksum, replaces the running binary, and
restarts itself.

## Installation

Install to a directory you own so the program can replace itself on update.
`~/.local/bin` is the recommended location on Linux and macOS:

```bash
make build VERSION=1.0.0
mv bin/nametag ~/.local/bin/nametag
```

Make sure `~/.local/bin` is on your `PATH`. If it isn't, add this to your shell profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Building locally

```bash
# Dev build — auto-update is disabled
go build ./cmd/nametag
```

## Releasing

Create a release through the GitHub UI (or however you prefer to tag). The CI
workflow will build all platform binaries, generate `checksums.txt`, and attach
them to the release automatically.

The release must include assets named:

| Platform       | Asset name                   |
|----------------|------------------------------|
| Linux amd64    | `nametag-linux-amd64`        |
| Linux arm64    | `nametag-linux-arm64`        |
| macOS amd64    | `nametag-darwin-amd64`       |
| macOS arm64    | `nametag-darwin-arm64`       |
| Windows amd64  | `nametag-windows-amd64.exe`  |

Plus a `checksums.txt` (SHA-256) for integrity verification.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-interval` | `1h` | How often to poll for updates. Accepts `s`, `m`, or `h` units. |

```bash
nametag -interval 30m
```

## Testing

```bash
make test
```
