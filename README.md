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

# Show available releases; the currently installed one is marked "*"
goup list
goup list --all -n 20    # include beta/rc, show 20 rows

# Requires write access to /usr/local, so run with sudo
sudo goup update              # jump to the latest stable
sudo goup install 1.25.11     # pin a specific version (also accepts go1.25.11)
sudo goup install 1.27rc1 --pre    # opt into a pre-release

# Manually restore the previous version (also requires sudo)
sudo goup rollback

# Show goup's own version, target platform, and the Go used to build it
goup version
```

Write commands (`update`, `install`, `rollback`) fail fast with a `permission denied` message before downloading anything if `/usr/local` isn't writable, so you never waste bandwidth on an unauthorized attempt. `goup` never elevates privileges itself.

### `goup check`

Prints the currently installed Go version and the latest stable version published by go.dev. It never writes to disk.

### `goup list`

Prints available Go releases newest first, marking the installed version with `*`. If the installed toolchain falls outside the display window, it is still shown after a `  ...` separator so the command never hides "what am I running". Flags:

- `--all` include beta/rc pre-releases (default: stable only)
- `-n <N>` cap the number of rows (default: 10)

### `goup update`

1. Fetches the latest stable release metadata (filename, sha256) from `https://go.dev/dl/?mode=json`
2. Exits immediately if it already matches the current version
3. Downloads the tarball to a temporary directory and verifies it with `crypto/sha256`. **If verification fails, extraction never happens and the command exits with an error**
4. Renames `/usr/local/go` to `/usr/local/go.bak.<unixtimestamp>` (only the latest backup generation is ever kept)
5. Extracts the new tarball into `/usr/local`
6. Launches the newly extracted `go` binary to confirm it starts. **On failure, it automatically rolls back to the previous version from the backup**
7. On success, the backup is left in place (not deleted automatically, so `goup rollback` can still restore it)

### `goup install <version>`

Pins a specific Go version, e.g. `sudo goup install 1.25.11`. Accepts either `1.25.11` or `go1.25.11` (case-insensitive). Uses the same download → verify → backup → extract → launch → auto-rollback sequence as `goup update`.

Pre-releases (`1.27rc1`, etc.) are refused unless you pass `--pre`, matching the default stable-only stance of `goup update`. Passing a version that equals the current install is a no-op.

### `goup rollback`

Manually restores `/usr/local/go` from the most recent backup left by `goup update` or `goup install`. Fails if no backup exists. Prints the version it rolled back to so you can confirm.

### `goup version`

Prints the `goup` release tag, target OS/arch, and the Go version it was built with. Reports `dev` for local `go build` output; release binaries have the tag baked in via `-ldflags "-X main.version=..."`.

## Requirements

- OS: WSL2 (Ubuntu) or macOS (Apple Silicon). Windows is not supported and the command exits with a clear message instead of crashing
- Go must already be installed at `/usr/local/go` via the official tarball layout
- `update` and `rollback` require write access to `/usr/local` (sudo)

## Out of scope

- Actually performing an update on native Windows (it only prints a message and exits)
- Managing multiple Go versions side by side (no gvm/goenv-style version switching — use `mise` / `asdf` / `gvm` for that)
- Reading or rewriting the `toolchain` directive in `go.mod` (`goup` only manages the `/usr/local/go` install itself; the current version is read from `/usr/local/go/VERSION`, which is unaffected by any nearby `go.mod`)
- First-time installation to a machine with no existing `/usr/local/go` (follow the [official Go install instructions](https://go.dev/doc/install) once, then use `goup` from then on)
- Automatic sudo elevation (it only prompts you to rerun with sudo)
- Multi-generation backups (only the latest is retained; use `goup rollback` immediately after a bad update)

## Development

No external dependencies — stdlib only.

```sh
go build ./...
go vet ./...
go test ./...
govulncheck ./...
```
