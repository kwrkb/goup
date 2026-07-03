# goup

`goup` is a CLI that safely updates the Go toolchain installed at `/usr/local/go` via the official tarball layout to the latest stable release.

Targeting WSL2 (Ubuntu) and macOS (Apple Silicon), it runs download → sha256 verification → backup → extraction → launch check → automatic rollback on failure, all in a single command.

## Install

```sh
go install github.com/kwrkb/goup@latest
```

If your shell already defines a function or alias named `goup`, it will shadow this binary. Remove or rename it first.

## Usage

```sh
# Compare current and latest stable versions only (no side effects)
goup check

# Requires write access to /usr/local, so run with sudo
sudo goup update

# Manually restore the previous version (also requires sudo)
sudo goup rollback
```

### `goup check`

Prints the currently installed Go version and the latest stable version published by go.dev. It never writes to disk.

### `goup update`

1. Fetches the latest stable release metadata (filename, sha256) from `https://go.dev/dl/?mode=json`
2. Exits immediately if it already matches the current version
3. Downloads the tarball to a temporary directory and verifies it with `crypto/sha256`. **If verification fails, extraction never happens and the command exits with an error**
4. Renames `/usr/local/go` to `/usr/local/go.bak.<unixtimestamp>` (only the latest backup generation is ever kept)
5. Extracts the new tarball into `/usr/local`
6. Runs `go version` against the newly extracted install to confirm it launches. **On failure, it automatically rolls back to the previous version from the backup**
7. On success, the backup is left in place (not deleted automatically, so `goup rollback` can still restore it)

If `/usr/local` isn't writable, the command exits with an error message telling you to rerun with sudo. It never attempts to elevate privileges itself.

### `goup rollback`

Manually restores `/usr/local/go` from the most recent backup left by `goup update`. Fails if no backup exists.

## Requirements

- OS: WSL2 (Ubuntu) or macOS (Apple Silicon). Windows is not supported and the command exits with a clear message instead of crashing
- Go must already be installed at `/usr/local/go` via the official tarball layout
- `update` and `rollback` require write access to `/usr/local` (sudo)

## Out of scope

- Actually performing an update on native Windows (it only prints a message and exits)
- Managing multiple Go versions side by side (no gvm/goenv-style version switching)
- Reading or rewriting the `toolchain` directive in `go.mod` (`goup` only manages the `/usr/local/go` install itself; `CurrentVersion()` sets `GOTOOLCHAIN=local` so it isn't misled by a toolchain directive, but it does not touch `go.mod`)
- Automatic sudo elevation (it only prompts you to rerun with sudo)

## Development

No external dependencies — stdlib only.

```sh
go build ./...
go vet ./...
go test ./...
govulncheck ./...
```
